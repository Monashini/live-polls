// Command peek prints what is actually stored, so you can see the data
// without clicking through a dashboard.
//
// It reads backend/.env for connection details and prints NO credentials.
// Password hashes are redacted; everything else is your own poll data.
//
//	cd backend && go run ./cmd/peek
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	_ = godotenv.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ----------------------------------------------------------- MongoDB
	fmt.Println("==================== MONGODB (source of truth) ====================")
	client, err := mongo.Connect(options.Client().ApplyURI(os.Getenv("MONGODB_URI")))
	if err != nil {
		fmt.Println("mongo connect:", err)
		os.Exit(1)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	dbName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" {
		dbName = "livepolls"
	}
	db := client.Database(dbName)

	fmt.Printf("database: %s\n\n", dbName)

	for _, coll := range []string{"users", "polls", "votes"} {
		n, err := db.Collection(coll).CountDocuments(ctx, bson.D{})
		if err != nil {
			fmt.Printf("  %-8s error: %v\n", coll, err)
			continue
		}
		fmt.Printf("  %-8s %d document(s)\n", coll, n)
	}

	fmt.Println("\n--- newest 3 polls ---")
	cur, err := db.Collection("polls").Find(ctx, bson.D{},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(3))
	if err == nil {
		var polls []bson.M
		_ = cur.All(ctx, &polls)
		for _, p := range polls {
			fmt.Printf("  %v\n", p["question"])
			fmt.Printf("    slug=%v  mode=%v  closed=%v  counts=%v  ballots=%v\n",
				p["slug"], p["mode"], p["closed"], p["counts"], p["ballots"])
		}
	}

	fmt.Println("\n--- newest 3 votes (voterKey/ipHash redacted) ---")
	cur, err = db.Collection("votes").Find(ctx, bson.D{},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(3))
	if err == nil {
		var votes []bson.M
		_ = cur.All(ctx, &votes)
		for _, v := range votes {
			fmt.Printf("    pollId=%v  optionIndexes=%v  at=%v\n",
				v["pollId"], v["optionIndexes"], v["createdAt"])
		}
	}

	fmt.Println("\n--- users (password hashes redacted) ---")
	cur, err = db.Collection("users").Find(ctx, bson.D{},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(3))
	if err == nil {
		var users []bson.M
		_ = cur.All(ctx, &users)
		for _, u := range users {
			hash, _ := u["passwordHash"].(string)
			fmt.Printf("    %-34v passwordHash=<bcrypt, %d chars>\n", u["email"], len(hash))
		}
	}

	fmt.Println("\n--- indexes (the correctness constraints) ---")
	for _, coll := range []string{"users", "polls", "votes"} {
		iCur, err := db.Collection(coll).Indexes().List(ctx)
		if err != nil {
			continue
		}
		var idx []bson.M
		_ = iCur.All(ctx, &idx)
		for _, i := range idx {
			unique := ""
			if u, ok := i["unique"].(bool); ok && u {
				unique = "  UNIQUE"
			}
			fmt.Printf("    %-7s %-18v %v%s\n", coll, i["name"], i["key"], unique)
		}
	}

	// ------------------------------------------------------------- Redis
	fmt.Println("\n==================== REDIS (live layer) ====================")
	opts, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		fmt.Println("redis url:", err)
		return
	}
	rdb := redis.NewClient(opts)
	defer func() { _ = rdb.Close() }()

	iter := rdb.Scan(ctx, 0, "livepolls:*", 200).Iterator()
	count := 0
	for iter.Next(ctx) {
		key := iter.Val()
		typ, _ := rdb.Type(ctx, key).Result()
		ttl, _ := rdb.TTL(ctx, key).Result()
		fmt.Printf("\n  %s\n    type=%s  ttl=%s\n", key, typ, ttl)

		switch typ {
		case "hash":
			vals, _ := rdb.HGetAll(ctx, key).Result()
			for f, v := range vals {
				fmt.Printf("      %-8s = %s\n", f, v)
			}
		case "string":
			v, _ := rdb.Get(ctx, key).Result()
			fmt.Printf("      value    = %s\n", v)
		}
		count++
	}
	if count == 0 {
		fmt.Println("  (no keys - counters expire after 24h idle and rebuild from MongoDB on next read)")
	}
	fmt.Printf("\n  %d key(s)\n", count)
}
