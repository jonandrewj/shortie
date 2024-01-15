package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

type URLObject struct {
	ShortID    string           `dynamodbav:"shortID"`
	URL        string           `dynamodbav:"url"`
	Version    int64            `dynamodbav:"version"`
	Expiration int64            `dynamodbav:"expiration"`
	Usage      map[string]int64 `dynamodbav:"usage"`
}

type LocalStorage struct {
	Objects map[string]URLObject
	lock    sync.Mutex
}

func (storage *LocalStorage) SaveURL(ctx context.Context, shortID string, url string, expiration int64) error {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	_, found := storage.Objects[shortID]
	if found {
		return nil
	}
	storage.Objects[shortID] = URLObject{
		ShortID:    shortID,
		URL:        url,
		Expiration: expiration,
		Usage:      map[string]int64{},
	}
	return nil
}

func (storage *LocalStorage) GetURL(ctx context.Context, shortID string) (string, error) {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	object, found := storage.Objects[shortID]
	if !found {
		return "", nil
	}

	todayTimestamp := strconv.Itoa(int(UTCTimestampOfTodayRounded().Unix()))
	todayUsage := object.Usage[todayTimestamp]
	object.Usage[todayTimestamp] = todayUsage + 1
	storage.Objects[shortID] = object

	return object.URL, nil
}

func (storage *LocalStorage) DeleteURL(ctx context.Context, shortID string) error {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	delete(storage.Objects, shortID)

	return nil
}

func (storage *LocalStorage) GetStatistics(ctx context.Context, shortID string) (map[string]int64, error) {
	storage.lock.Lock()
	defer storage.lock.Unlock()
	object, found := storage.Objects[shortID]
	if !found {
		return map[string]int64{}, nil
	}
	return object.Usage, nil
}

func UTCTimestampOfTodayRounded() time.Time {
	return time.Now().UTC().Truncate(time.Hour * 24)
}

const tableName = "shortie-urls"
const attributeShortID = "shortID"

type DynamoStorage struct {
	dynamo *dynamodb.DynamoDB
}

func InitDynamoStorage(env Environment) (*DynamoStorage, error) {
	awsConfig := aws.NewConfig().
		WithRegion(env.AWSRegion).
		WithEndpoint(env.AWSCustomDynamoEndpoint).
		WithCredentials(credentials.NewStaticCredentials(
			env.AWSAccessKeyID,
			env.AWSSecretAccessKey,
			"",
		))

	awsSession, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize an aws session: %w", err)
	}
	dynamoClient := dynamodb.New(awsSession)
	return &DynamoStorage{
		dynamo: dynamoClient,
	}, nil
}

func (storage *DynamoStorage) InitializeTable() error {
	_, err := storage.dynamo.CreateTable(&dynamodb.CreateTableInput{
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String(attributeShortID),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
		},
		BillingMode:               aws.String(dynamodb.BillingModePayPerRequest),
		DeletionProtectionEnabled: aws.Bool(true),
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(attributeShortID),
				KeyType:       aws.String(dynamodb.KeyTypeHash),
			},
		},
		TableName: aws.String(tableName),
		Tags:      nil,
	})
	if err != nil {
		awsErr := err.(awserr.Error)
		if awsErr.Code() == dynamodb.ErrCodeTableAlreadyExistsException || awsErr.Code() == dynamodb.ErrCodeResourceInUseException {
			return nil
		}
		return fmt.Errorf("failed to create the table: %w", err)
	}

	// TODO: setup the auto-expiration if we want to keep that feature
	return nil
}

func (storage *DynamoStorage) SaveURL(ctx context.Context, shortID string, url string, expiration int64) error {
	object := URLObject{
		ShortID:    shortID,
		URL:        url,
		Version:    0,
		Expiration: expiration,
		Usage:      map[string]int64{},
	}
	dynamoItem, err := dynamodbattribute.MarshalMap(&object)
	if err != nil {
		return fmt.Errorf("failed to serialize url object: %w", err)
	}

	_, err = storage.dynamo.PutItemWithContext(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(tableName),
		Item:                dynamoItem,
		ConditionExpression: aws.String("attribute_not_exists(#shortID)"),
		ExpressionAttributeNames: map[string]*string{
			"#shortID": aws.String(attributeShortID),
		},
	})
	if err != nil {
		var awsErr awserr.Error
		if errors.As(err, &awsErr) && (awsErr.Code() == dynamodb.ErrCodeConditionalCheckFailedException) {
			return nil
		}
		return fmt.Errorf("failed to save a url: %w", err)
	}
	return nil
}

func (storage *DynamoStorage) GetURL(ctx context.Context, shortID string) (string, error) {
	object, err := storage.getObject(ctx, shortID)
	if err != nil {
		return "", err
	}
	if object == nil {
		return "", nil
	}

	go func() {
		// This is an absolutely horrible way to do this for scale reasons but works for low usage
		// With more time, I would buffer these updates in-memory (at risk of losing some occasionally)
		// and flush say a minutes worth of usage all in one request. Very similar to how metric infrastructure works.
		todayTimestamp := strconv.Itoa(int(UTCTimestampOfTodayRounded().Unix()))
		todayUsage := object.Usage[todayTimestamp]
		object.Usage[todayTimestamp] = todayUsage + 1

		serialized, err := dynamodbattribute.MarshalMap(&object)
		if err != nil {
			log.Println("failed to increment usage: " + err.Error())
		}

		_, err = storage.dynamo.PutItemWithContext(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(tableName),
			Item:      serialized,
		})
		if err != nil {
			log.Println("failed to increment usage: " + err.Error())
		}
	}()

	return object.URL, nil
}

func (storage *DynamoStorage) DeleteURL(ctx context.Context, shortID string) error {
	_, err := storage.dynamo.DeleteItemWithContext(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key: map[string]*dynamodb.AttributeValue{
			attributeShortID: {S: aws.String(shortID)},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete a url object: %w", err)
	}
	return nil
}

func (storage *DynamoStorage) GetStatistics(ctx context.Context, shortID string) (map[string]int64, error) {
	object, err := storage.getObject(ctx, shortID)
	if err != nil {
		return nil, err
	}
	return object.Usage, nil
}

func (storage *DynamoStorage) getObject(ctx context.Context, shortID string) (*URLObject, error) {
	out, err := storage.dynamo.GetItemWithContext(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]*dynamodb.AttributeValue{
			attributeShortID: {S: aws.String(shortID)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read a shortID: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	var object URLObject
	err = dynamodbattribute.UnmarshalMap(out.Item, &object)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize url object: %w", err)
	}
	if object.Usage == nil {
		object.Usage = map[string]int64{}
	}
	return &object, nil
}
