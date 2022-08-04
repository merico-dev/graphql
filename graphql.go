package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/merico-dev/graphql/internal/jsonutil"
	"golang.org/x/net/context/ctxhttp"
)

// Client is a GraphQL client.
type Client struct {
	url        string // GraphQL server URL.
	httpClient *http.Client
}

// NewClient creates a GraphQL client targeting the specified GraphQL server URL.
// If httpClient is nil, then http.DefaultClient is used.
func NewClient(url string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		url:        url,
		httpClient: httpClient,
	}
}

// Query executes a single GraphQL query request,
// with a query derived from q, populating the response into it.
// q should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Query(ctx context.Context, q interface{}, variables map[string]interface{}) error {
	query, variables := ConstructQuery(q, variables)
	// TODO: Consider including response body in returned error, if deemed helpful.
	data, dataErr := c.do(ctx, query, q, variables)
	if data != nil {
		// merge XXX__N to XXX as a slice
		rawData := map[string]interface{}{}
		s, err := data.MarshalJSON()
		if err != nil {
			// TODO: Consider including response body in returned error, if deemed helpful.
			return err
		}
		err = json.Unmarshal(s, &rawData)
		if err != nil {
			// TODO: Consider including response body in returned error, if deemed helpful.
			return err
		}
		for k, v := range rawData {
			index := strings.Index(k, `__`)
			if index != -1 {
				subList, ok := rawData[k[:index]]
				if ok {
					rawData[k[:index]] = append(subList.([]interface{}), v)
				} else {
					rawData[k[:index]] = []interface{}{v}
				}
				delete(rawData, k)
			}
		}
		data, err := json.Marshal(rawData)
		if err != nil {
			// TODO: Consider including response body in returned error, if deemed helpful.
			return err
		}
		err = jsonutil.UnmarshalGraphQL(data, q)
		if err != nil {
			// TODO: Consider including response body in returned error, if deemed helpful.
			return err
		}
	}
	return dataErr
}

// Mutate executes a single GraphQL mutation request,
// with a mutation derived from m, populating the response into it.
// m should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Mutate(ctx context.Context, m interface{}, variables map[string]interface{}) error {
	query := ConstructMutation(m, variables)
	// TODO: Consider including response body in returned error, if deemed helpful.
	data, dataErr := c.do(ctx, query, m, variables)
	if data != nil {
		err := jsonutil.UnmarshalGraphQL(*data, m)
		if err != nil {
			// TODO: Consider including response body in returned error, if deemed helpful.
			return err
		}
	}
	return dataErr
}

// do executes a single GraphQL operation.
func (c *Client) do(ctx context.Context, query string, v interface{}, variables map[string]interface{}) (*json.RawMessage, error) {
	in := struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables,omitempty"`
	}{
		Query:     query,
		Variables: variables,
	}
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(in)
	if err != nil {
		return nil, err
	}
	resp, err := ctxhttp.Post(ctx, c.httpClient, c.url, "application/json", &buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("non-200 OK status code: %v body: %q", resp.Status, body)
	}
	var out struct {
		Data   *json.RawMessage
		Errors errors
		//Extensions interface{} // Unused.
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	if err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return out.Data, out.Errors
	}
	return out.Data, nil
}

// errors represents the "errors" array in a response from a GraphQL server.
// If returned via error interface, the slice is expected to contain at least 1 element.
//
// Specification: https://facebook.github.io/graphql/#sec-Errors.
type errors []struct {
	Message   string
	Locations []struct {
		Line   int
		Column int
	}
}

// Error implements error interface.
func (e errors) Error() string {
	return e[0].Message
}
