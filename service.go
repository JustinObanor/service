package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// toStore stores the details of an object to psql
func (db *database) toStore(ctx context.Context, detail ObjectDetail) error {
	query := "insert into objects (id, online, lastseen) values($1, $2, $3)"

	if _, err := db.db.ExecContext(ctx, query, detail.ID, detail.Online, detail.LastSeen); err != nil {
		return fmt.Errorf("error inserting object details: %v", err)
	}
	return nil
}

func (db *database) deleteDetail(ctx context.Context) {
	for range time.NewTicker(time.Second * 5).C {
		query := "delete from objects where lastseen < now() - interval '30 seconds'"

		if _, err := db.db.ExecContext(ctx, query); err != nil {
			db.errChan <- err
			continue
		}
	}
}

// fetchDetail calls localhost:9010/objects/id to receive details of an object by its id
func (c *client) fetchDetail(ctx context.Context, objectID int) (ObjectDetail, error) {
	path := c.path + strconv.Itoa(objectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, nil)
	if err != nil {
		return ObjectDetail{}, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return ObjectDetail{}, err
	}
	defer resp.Body.Close()

	detail := ObjectDetail{}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return ObjectDetail{}, err
	}

	detail.LastSeen = time.Now()

	return detail, nil
}
