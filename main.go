package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
)

type Environment struct {
	AWSRegion               string
	AWSAccessKeyID          string
	AWSSecretAccessKey      string
	AWSCustomDynamoEndpoint string
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		<-ctx.Done()
		// TODO: set this up so that the gin.Router can have a little time to finish requests
		os.Exit(0)
	}()

	var env = Environment{
		AWSRegion:               os.Getenv("AWS_REGION"),
		AWSAccessKeyID:          os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretAccessKey:      os.Getenv("AWS_SECRET_ACCESS_KEY"),
		AWSCustomDynamoEndpoint: os.Getenv("AWS_CUSTOM_DYNAMO_ENDPOINT"),
	}

	// in-memory storage if dynamo is not configured to be used
	var storage urlStorage = &LocalStorage{
		Objects: map[string]URLObject{},
		lock:    sync.Mutex{},
	}

	// set up a dynamo backend
	//  - good for high reads/writes
	//  - has auto-expiration and atomic incrementation for usage statistics
	//  - can easily enable global replication
	//  - can enable dynamo's caching layer in addition to our own local cache
	if env.AWSCustomDynamoEndpoint != "" {
		log.Println("using dynamodb backend")
		dynamoClient, err := InitDynamoStorage(env)
		if err != nil {
			log.Println("error: " + err.Error())
			panic(err)
		}
		err = dynamoClient.InitializeTable()
		if err != nil {
			log.Println("error: " + err.Error())
			panic(err)
		}
		storage = dynamoClient
	} else {
		log.Println("using in-memory backend")
	}

	// TODO: setup a local caching layer with the storage interface to optimize
	//  - comes with the potential caveat of deletes not propagating immediately

	api := shortieAPI{storage: storage}

	router := api.GetRouter()

	err := router.Run(":8421")
	if err != nil {
		log.Printf("exiting: %s\n", err.Error())
	}
}
