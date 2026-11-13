package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"
)

var databaseName string

func Connect(uri string) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cs, err := connstring.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse MongoDB URI: %w", err)
	}
	databaseName = cs.Database
	if databaseName == "" {
		return nil, fmt.Errorf("Invalid mongo MongoDB URI: Database name is empty")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	log.Printf("Connected to MongoDB: %s", cs.Database)
	return client, nil
}

func Collection(client *mongo.Client, name string) *mongo.Collection {
	return client.Database(databaseName).Collection(name)
}

func EnsureIndexes(client *mongo.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	collections := []struct {
		name    string
		indexes []mongo.IndexModel
	}{
		{
			name: "users",
			indexes: []mongo.IndexModel{
				{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
				{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)},
				{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)},
			},
		},
		{
			name: "catalog",
			indexes: []mongo.IndexModel{
				{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
				{Keys: bson.D{{Key: "type", Value: 1}}},
			},
		},
		{
			name: "viewers",
			indexes: []mongo.IndexModel{
				{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
				{Keys: bson.D{{Key: "user_uuid", Value: 1}}},
			},
		},
		{
			name: "categories",
			indexes: []mongo.IndexModel{
				{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
			},
		},
		{
			name: "token_blacklist",
			indexes: []mongo.IndexModel{
				{Keys: bson.D{{Key: "jti", Value: 1}}, Options: options.Index().SetUnique(true)},
			},
		},
	}

	for _, c := range collections {
		coll := Collection(client, c.name)
		for _, idx := range c.indexes {
			if _, err := coll.Indexes().CreateOne(ctx, idx); err != nil {
				log.Printf("Warning: failed to create index on %s: %v", c.name, err)
			}
		}
	}

	log.Println("Database indexes ensured")
}
