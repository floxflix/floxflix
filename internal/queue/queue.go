package queue

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/floxflix/floxflix/internal/config"
	"github.com/floxflix/floxflix/internal/database"
	"github.com/floxflix/floxflix/internal/storage"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

type Job struct {
	UUID            string     `json:"uuid"`
	InputPath       string     `json:"input_path"`
	OutputDir       string     `json:"output_dir"`
	OutputName      string     `json:"output_name"`
	ContentType     string     `json:"content_type"` // "movie" or "tvshow"
	ThumbailPath    string     `json:"thumbail_path"`
	DurationSeconds float64    `json:"duration_seconds,omitempty"`
	Season          int        `json:"season,omitempty"`
	Episode         int        `json:"episode,omitempty"`
	Status          JobStatus  `json:"status"`
	Error           string     `json:"error,omitempty"`
	QueuedAt        time.Time  `json:"queued_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type ConversionQueue struct {
	mu      sync.Mutex
	jobs    []Job
	running bool
	client  *mongo.Client
	cfg     *config.Config
}

func New(client *mongo.Client, cfg *config.Config) *ConversionQueue {
	return &ConversionQueue{
		client: client,
		cfg:    cfg,
	}
}

func (q *ConversionQueue) Add(job Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job.Status = StatusPending
	job.QueuedAt = time.Now()
	q.jobs = append(q.jobs, job)
}

func (q *ConversionQueue) Start() {
	q.mu.Lock()
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()

	go q.processLoop()
}

func (q *ConversionQueue) Info() map[string]any {
	q.mu.Lock()
	defer q.mu.Unlock()

	pending, processing, completed, failed := 0, 0, 0, 0
	for _, j := range q.jobs {
		switch j.Status {
		case StatusPending:
			pending++
		case StatusProcessing:
			processing++
		case StatusCompleted:
			completed++
		case StatusFailed:
			failed++
		}
	}

	return map[string]any{
		"running":    q.running,
		"total":      len(q.jobs),
		"pending":    pending,
		"processing": processing,
		"completed":  completed,
		"failed":     failed,
		"jobs":       q.jobs,
	}
}

func (q *ConversionQueue) Cleanup() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	var remaining []Job
	removed := 0
	for _, j := range q.jobs {
		if j.Status == StatusCompleted || j.Status == StatusFailed {
			removed++
		} else {
			remaining = append(remaining, j)
		}
	}
	q.jobs = remaining
	return removed
}

func (q *ConversionQueue) processLoop() {
	for {
		q.mu.Lock()
		var job *Job
		for i := range q.jobs {
			if q.jobs[i].Status == StatusPending {
				q.jobs[i].Status = StatusProcessing
				job = &q.jobs[i]
				break
			}
		}
		q.mu.Unlock()

		if job == nil {
			time.Sleep(2 * time.Second)
			continue
		}

		err := q.processJob(job)

		q.mu.Lock()
		now := time.Now()
		for i := range q.jobs {
			// One catalog item can have several jobs queued at once (an episode
			// per POST, or a whole season), so the UUID alone does not identify
			// the one that just finished.
			if q.jobs[i].UUID == job.UUID && q.jobs[i].Season == job.Season && q.jobs[i].Episode == job.Episode {
				if err != nil {
					q.jobs[i].Status = StatusFailed
					q.jobs[i].Error = err.Error()
				} else {
					q.jobs[i].Status = StatusCompleted
				}
				q.jobs[i].CompletedAt = &now
				break
			}
		}
		q.mu.Unlock()
	}
}

// thumbnailSeekOffset picks ~10% into the title so the frame is past
// opening blacks/logos. Falls back to 5s when duration is unknown.
func thumbnailSeekOffset(duration time.Duration) time.Duration {
	if duration <= 0 {
		return 5 * time.Second
	}
	offset := time.Duration(float64(duration) * 0.1)
	// Keep the seek inside the file for very short clips.
	if offset >= duration {
		offset = duration / 10
		if offset <= 0 {
			offset = duration / 2
		}
	}
	return offset
}

// formatSeekTimestamp formats a duration as HH:MM:SS.mmm for ffmpeg -ss.
func formatSeekTimestamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalMs := d.Milliseconds()
	h := totalMs / 3_600_000
	m := (totalMs % 3_600_000) / 60_000
	s := (totalMs % 60_000) / 1000
	ms := totalMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

// thumbnailFFmpegArgs builds ffmpeg args with input seek (-ss before -i).
func thumbnailFFmpegArgs(inputPath, thumbnailPath string, duration time.Duration) []string {
	ss := formatSeekTimestamp(thumbnailSeekOffset(duration))
	return []string{
		"-y",
		"-ss", ss,
		"-i", inputPath,
		"-vframes", "1",
		thumbnailPath,
	}
}

func (q *ConversionQueue) processJob(job *Job) error {
	log.Printf("Processing conversion for %s: %s", job.UUID, job.InputPath)

	fullPath := storage.Resolve(q.cfg.UploadFolder, job.OutputDir)

	if err := os.MkdirAll(fullPath, 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	info := probeMedia(job.InputPath)

	thumbnailPath := filepath.Join(fullPath, storage.ThumbnailName)
	thumbCmd := exec.Command("ffmpeg", thumbnailFFmpegArgs(job.InputPath, thumbnailPath, info.Duration)...)
	if out, err := thumbCmd.CombinedOutput(); err != nil {
		log.Printf("Thumbnail generation warning: %v, output: %s", err, string(out))
	}
	job.ThumbailPath = filepath.Join(job.OutputDir, storage.ThumbnailName)

	outputPath := filepath.Join(fullPath, storage.PlaylistFile(job.OutputName))
	segmentTime := fmt.Sprintf("%d", q.cfg.HLSSegmentTime)
	listSize := fmt.Sprintf("%d", q.cfg.HLSListSize)

	canCopyVideo := isSafeToCopyVideo(job.InputPath, info, q.cfg.HLSSegmentTime)

	videoArgs := []string{
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		"-sc_threshold", "0",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", q.cfg.HLSSegmentTime),
	}

	if canCopyVideo {
		videoArgs = []string{"-c:v", "copy"}
		if q.cfg.HLSSegmentType == "mpegts" {
			videoArgs = append(videoArgs, "-bsf:v", "h264_mp4toannexb")
		}
	}

	audioArgs := []string{"-c:a", "aac", "-b:a", "128k", "-ac", "2"}
	if info.AudioCodec == "aac" {
		audioArgs = []string{"-c:a", "copy"}

		if q.cfg.HLSSegmentType == "fmp4" {
			audioArgs = append(audioArgs, "-bsf:a", "aac_adtstoasc")
		}
	}

	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", job.InputPath,
		"-map", "0:v:0",
		"-map", "0:a:0?", // el '?' evita error si no hay audio
	}
	args = append(args, videoArgs...)
	args = append(args, audioArgs...)

	args = append(args,
		"-f", "hls",
		"-hls_time", segmentTime,
		"-hls_list_size", listSize,
		"-hls_segment_type", q.cfg.HLSSegmentType,
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(fullPath, storage.SegmentPattern(job.OutputName, q.segmentExt())),
		outputPath,
	)

	cmd := exec.Command("ffmpeg", args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("ffmpeg conversion failed: %v, output: %s", err, string(out))
		// Job.Error is served by the queue info endpoint: keep the tail,
		// where ffmpeg says why it failed. The full output is logged above.
		return fmt.Errorf("ffmpeg conversion failed: %w, output: %s", err, ffmpegErrorTail(out))
	}

	job.DurationSeconds = info.Duration.Seconds()
	if job.DurationSeconds <= 0 {
		if secs, err := PlaylistDurationSeconds(outputPath); err != nil {
			log.Printf("duration unknown for %s: %v", job.UUID, err)
		} else {
			job.DurationSeconds = secs
		}
	}

	// Remove original file
	os.Remove(job.InputPath)

	// Update catalog status
	q.updateCatalogStatus(job)

	log.Printf("Conversion completed for %s", job.UUID)
	return nil
}

func (q *ConversionQueue) segmentExt() string {
	if q.cfg.HLSSegmentType == "fmp4" {
		return "m4s"
	}
	return "ts"
}

func (q *ConversionQueue) updateCatalogStatus(job *Job) {
	coll := database.Collection(q.client, "catalog")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hlsPath := filepath.Join(job.OutputDir, storage.PlaylistFile(job.OutputName))

	setFields := bson.M{
		"status":     "Ready",
		"updated_at": time.Now(),
	}

	if job.ThumbailPath != "" {
		var current struct {
			BGImage string `bson:"bg_image"`
		}
		if err := coll.FindOne(ctx, bson.M{"uuid": job.UUID}).Decode(&current); err != nil {
			log.Printf("catalog lookup failed for %s: %v", job.UUID, err)
		} else if current.BGImage == "" {
			setFields["bg_image"] = job.ThumbailPath
		}
	}

	if job.ContentType == "movie" {
		setFields["file_path"] = hlsPath
		if job.DurationSeconds > 0 {
			setFields["duration_seconds"] = job.DurationSeconds
		}
		result, err := coll.UpdateOne(ctx,
			bson.M{"uuid": job.UUID},
			bson.M{"$set": setFields},
		)
		if err != nil {
			log.Printf("catalog update failed for %s: %v", job.UUID, err)
		} else if result.MatchedCount == 0 {
			log.Printf("Warning: no catalog entry found for %s", job.UUID)
		}
	} else {
		setFields["seasons.$[s].episodes.$[e].status"] = "Ready"
		setFields["seasons.$[s].episodes.$[e].file_path"] = hlsPath
		setFields["seasons.$[s].episodes.$[e].thumbail_path"] = job.ThumbailPath
		if job.DurationSeconds > 0 {
			setFields["seasons.$[s].episodes.$[e].duration_seconds"] = job.DurationSeconds
		}

		result, err := coll.UpdateOne(ctx,
			bson.M{"uuid": job.UUID},
			bson.M{"$set": setFields},
			options.UpdateOne().SetArrayFilters([]interface{}{
				bson.M{"s.season_number": job.Season},
				bson.M{"e.episode_number": job.Episode},
			}),
		)

		if err != nil {
			log.Printf("catalog update failed: %v", err)
		} else if result.MatchedCount == 0 {
			log.Printf("Warning: no catalog entry found for %s S%dE%d", job.UUID, job.Season, job.Episode)
		}
	}
}

// isSafeToCopyVideo strictly verifies that an H.264 stream meets all HLS & MSE compatibility requirements.
func isSafeToCopyVideo(inputPath string, info mediaInfo, hlsSegmentTime int) bool {

	if info.VideoCodec != "h264" || info.PixFmt != "yuv420p" {
		return false
	}

	log.Printf("FieldOrder: %s", info.FieldOrder)

	if info.FieldOrder != "" && info.FieldOrder != "progressive" {
		return false
	}

	allowedProfiles := map[string]bool{
		"Baseline":             true,
		"Constrained Baseline": true,
		"Main":                 true,
		"High":                 true,
	}

	if !allowedProfiles[info.VideoProfile] || info.Level > 42 {
		return false
	}

	x264Header := extractX264Header(inputPath)
	if x264Header == "" {
		return false
	}

	if !strings.Contains(x264Header, "vbv_maxrate=") || !strings.Contains(x264Header, "vbv_bufsize=") {
		return false
	}

	if strings.Contains(x264Header, "open_gop=1") || strings.Contains(x264Header, "interlaced=1") {
		return false
	}

	keyint := parseIntOption(x264Header, `keyint=(\d+)`)
	if keyint <= 0 || info.FrameRate <= 0 {
		return false
	}

	maxAllowedKeyint := int(math.Ceil(info.FrameRate * float64(hlsSegmentTime)))
	return keyint <= maxAllowedKeyint
}

// extractX264Header scans the initial bytes of the file for the x264 encoder options string.
func extractX264Header(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 65536) // 64KB is sufficient to capture the SEI user data NAL unit
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return ""
	}

	marker := []byte("x264 - core")
	idx := bytes.Index(buf[:n], marker)
	if idx == -1 {
		return ""
	}

	// Extract up to the null terminator
	end := bytes.IndexByte(buf[idx:n], 0)
	if end == -1 {
		return string(buf[idx:n])
	}
	return string(buf[idx : idx+end])
}

func parseIntOption(header, pattern string) int {
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(header)
	if len(matches) < 2 {
		return -1
	}
	val, err := strconv.Atoi(matches[1])
	if err != nil {
		return -1
	}
	return val
}
