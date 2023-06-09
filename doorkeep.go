package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/guregu/dynamo"
)

type Response struct {
	Results []Result `json:"items"`
}

type Result struct {
	Title         string `json:"title"`
	Link          string `json:"link"`
	DisplayedLink string `json:"displayLink"`
	Snippet       string `json:"snippet"`
}

type Query struct {
	Title string `dynamo:"title"`
	Link  string `dynamo:"link"`
}

type Item struct {
	Title         string `dynamo:"title"`
	Link          string `dynamo:"link"`
	DisplayedLink string `dynamo:"displayLink"`
	Snippet       string `dynamo:"snippet"`
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
		return fmt.Errorf("error creating message to send to Slack: %s", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("error sending Slack Message: %s", err)
	}
	defer resp.Body.Close()
	return nil
}

func getQueries() []string {
	queries := []string{
		"site%3Astackoverflow.com+OR+site%3Astackexchange.com+floqast+OR+liljwty",
		"%22liljwty%22",
		"%22x-liljwty-gate%22",
	}
	return queries
}

func parseResult(responseObject Response) error {
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
		return fmt.Errorf("failed to create AWS session: %s", err)
	}

	db := dynamo.New(sess)
	table := db.Table("doorkeep-dev")

	for i := 0; i < len(responseObject.Results); i++ {
		result := responseObject.Results[i]
		//Try to get each result from the db
		var response Query
		err := table.Get("title", result.Title).Range("link", dynamo.Equal, result.Link).One(&response)
		if err == dynamo.ErrNotFound {
			//Put the Result in the db if it isn't found
			item := Item{
				Title:         result.Title,
				Link:          result.Link,
				DisplayedLink: result.DisplayedLink,
				Snippet:       result.Snippet,
			}
			err := table.Put(item).Run()
			if err != nil {
				return fmt.Errorf("failed to put the result in the DB: %s", err)
			}
			//Send the new result as an alert
			err = notify(result)
			if err != nil {
				return fmt.Errorf("failed to send Slack notification: %s", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to get the result from the DB: %s", err)
		}
	}
	return nil
}

func handleRequest(ctx context.Context, event events.CloudWatchEvent) error {
	key := os.Getenv("SERP")
	searchEngineId := os.Getenv("SEID")
	queries := getQueries()
	for index := range queries {
		url := "https://www.googleapis.com/customsearch/v1?key=" + key + "&cx=" + searchEngineId + "&q=" + queries[index]
		response, err := http.Get(url)

		if err != nil {
			return fmt.Errorf("failed to get Search API response: %s", err)
		}

		responseData, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf("failed to read response from Search API: %s", err)
		}

		var responseObject Response
		json.Unmarshal(responseData, &responseObject)

		parseResult(responseObject)
	}
	return nil
}

func main() {
	lambda.Start(handleRequest)
}
