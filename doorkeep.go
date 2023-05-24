package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
)

type Response struct {
	Results []Result `json:"organic_results"`
}

type Result struct {
	Position          int    `json:"position"`
	Title             string `json:"title"`
	Link              string `json:"link"`
	DisplayedLink     string `json:"displayed_link"`
	Snippet           string `json:"snippet"`
	SnippetHighlights string `json:"snippet_highlighted_words"`
	CachedLink        string `json:"cached_page_link"`
	Source            string `json:"source"`
}

type Message struct {
	Blocks []Block `json:"blocks"`
}

type Block struct {
	Type string `json:"type"`
	Text Text   `json:"text"`
}

type Text struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func notify(result Result) error {
	url := os.Getenv("SLACK")
	blocks := []Block{
		{
			Type: "section",
			Text: Text{
				Type: "mrkdwn",
				Text: "*New Alert!*:\n",
			},
		},
		{
			Type: "section",
			Text: Text{
				Type: "mrkdwn",
				Text: fmt.Sprintf("<%s|%s>\n%s",
					result.Link,
					result.Title,
					result.Snippet,
				),
			},
		},
	}

	message := Message{
		Blocks: blocks,
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("Error creating message to send to Slack: %s", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("Error sending Slack Message: %s", err)
	}
	defer resp.Body.Close()
	return nil
}

func handleRequest(ctx context.Context, event events.CloudWatchEvent) error {

	// Initialize a session that the SDK will use to load
	// credentials from the shared credentials file ~/.aws/credentials
	// and region from the shared configuration file ~/.aws/config.
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region:      aws.String("us-east-1"),
			Credentials: credentials.NewStaticCredentials(os.Getenv("ACCESS_KEY"), os.Getenv("SECRET_ACCESS_KEY"), ""),
		},
	})
	if err != nil {
		return fmt.Errorf("Failed to create AWS session: %s", err)
	}

	// Create DynamoDB client
	svc := dynamodb.New(sess)

	key := os.Getenv("SERP")
	query := "site%3Astackoverflow.com+OR+site%3Astackexchange.com+floqast+OR+liljwty&google_domain"
	url := "https://serpapi.com/search.json?engine=google&q=" + query + "=google.com&gl=us&hl=en&api_key=" + key
	response, err := http.Get(url)

	if err != nil {
		return fmt.Errorf("Failed to get SerpAPI response: %s", err)
	}

	responseData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("Failed to read response from SerpAPI: %s", err)
	}

	var responseObject Response
	json.Unmarshal(responseData, &responseObject)

	for i := 0; i < len(responseObject.Results); i++ {
		result := responseObject.Results[i]
		//Try to get each result from the db
		response, err := svc.GetItemWithContext(ctx, &dynamodb.GetItemInput{
			TableName: aws.String("doorkeep"),
			Key: map[string]*dynamodb.AttributeValue{
				"position": {
					S: aws.String(strconv.Itoa(result.Position)),
				},
				"title": {
					S: aws.String(result.Title),
				},
			},
		})
		if err != nil {
			return fmt.Errorf("Failed to put the result in the DB: %s", err)
		}

		if response.Item == nil || len(response.Item) == 0 {
			//Put the Result in the db if it isn't found
			_, err := svc.PutItemWithContext(ctx, &dynamodb.PutItemInput{
				TableName: aws.String("doorkeep"),
				Item: map[string]*dynamodb.AttributeValue{
					"position": {
						S: aws.String(strconv.Itoa(result.Position)),
					},
					"title": {
						S: aws.String(result.Title),
					},
					"link": {
						S: aws.String(result.Link),
					},
					"displayedLink": {
						S: aws.String(result.DisplayedLink),
					},
					"snippet": {
						S: aws.String(result.Snippet),
					},
					"snippetHighlights": {
						S: aws.String(result.SnippetHighlights),
					},
					"cachedLink": {
						S: aws.String(result.CachedLink),
					},
					"source": {
						S: aws.String(result.Source),
					},
				},
			})
			if err != nil {
				return fmt.Errorf("Failed to put the result in the DB: %s", err)
			}
			//Send the new result as an alert
			err = notify(result)
			if err != nil {
				return fmt.Errorf("Failed to send Slack notification: %s", err)
			}
		}
	}
	return nil
}

func main() {
	lambda.Start(handleRequest)
}
