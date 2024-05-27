package main

import (
	"context"
	"encoding/json"
	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson"
	"log"
	"os"
	"strconv"
	"time"

	_ "go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type StockMessage struct {
	Company   string  `json:"company"`
	EventType string  `json:"eventType"`
	Price     float64 `json:"price"`
}

// failOnError is a helper function to log error messages
func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

// getEnvWithDefault returns the value of an environment variable or a default value if the environment variable is not set
func getEnvWithDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func connectToMongo() *mongo.Collection {
	mongoURI := getEnvWithDefault("MONGODB_URI", "mongodb://localhost:27017")
	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(context.TODO(), clientOptions)
	failOnError(err, "Failed to connect to MongoDB")

	// Test connection with Ping
	err = client.Ping(context.TODO(), nil)
	failOnError(err, "Failed to ping MongoDB")

	collection := client.Database("stockmarket").Collection("stocks")
	return collection
}

func calculateAverage(prices []float64) float64 {
	total := 0.0
	for _, price := range prices {
		total += price
	}
	return total / float64(len(prices))
}

func saveAverageToMongo(collection *mongo.Collection, average float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	document := bson.M{
		"company":  getEnvWithDefault("STOCK", "TSLA"),
		"avgPrice": average,
	}

	_, err := collection.InsertOne(ctx, document)
	failOnError(err, "Failed to insert document into MongoDB")
}

func stockReader(conn *amqp.Connection, collection *mongo.Collection) {
	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		getEnvWithDefault("STOCK", "TSLA"), // name of the queue to consume
		false,
		false,
		false,
		false,
		nil,
	)

	batchSizeValue := getEnvWithDefault("BATCH_SIZE", "1000")
	batchSize, err := strconv.Atoi(batchSizeValue)
	failOnError(err, "Failed to parse ticker interval")

	// Consume msgs from Que
	for {
		var prices []float64

		msgs, err := ch.Consume(
			q.Name,
			"",
			true, // Auto-Acknowledge -> Delete message when successfully consumed
			false,
			false,
			false,
			nil,
		)
		failOnError(err, "Failed to declare a queue")

		for i := 0; i < batchSize; i++ {
			msg := <-msgs

			var stockMessage StockMessage
			err := json.Unmarshal(msg.Body, &stockMessage)
			failOnError(err, "Failed to unmarshal message")

			prices = append(prices, stockMessage.Price)
		}

		average := calculateAverage(prices)
		saveAverageToMongo(collection, average)
	}

}

func main() {
	rabbitMQConnectionString := getEnvWithDefault("RABBITMQ_URL", "amqp://stockmarket:supersecret123@localhost:5672/")

	conn, err := amqp.Dial(rabbitMQConnectionString)

	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	collection := connectToMongo()

	go stockReader(conn, collection)

	log.Printf("All stock publishers are running. Press CTRL+C to exit.")
	select {}
}
