package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// toStore stores the details of an object to psql
func toStore(ctx context.Context, detail ObjectDetail) error {
	if db == nil {
		ctx.Done()
		return errors.New("db instance is nil")
	}

	query := fmt.Sprintf("insert into %s (id, online, lastseen) values($1, $2, $3)", dbname)

	if _, err := db.ExecContext(ctx, query, detail.ID, detail.Online, detail.LastSeen); err != nil {
		return fmt.Errorf("error inserting object details: %v", err)
	}
	return nil
}

// fromStore fetches all the objects from the database
func fromStore(ctx context.Context) ([]ObjectDetail, error) {
	if db == nil {
		ctx.Done()
		return nil, errors.New("db instance is nil")
	}

	rows, err := db.Query(fmt.Sprintf("select * from %s", dbname))
	if err != nil {
		ctx.Done()
		return nil, err
	}
	defer rows.Close()

	ls := ""
	detail := ObjectDetail{}
	objectDetails := make([]ObjectDetail, 0)

	for rows.Next() {
		if err := rows.Scan(&detail.ID, &detail.Online, &ls); err != nil {
			if err == sql.ErrNoRows {
				log.Println("no rows: ", err)
				break
			}
			return nil, err
		}

		lastSeen, err := time.Parse(time.RFC3339, strings.Replace(ls, " ", "T", 1))
		if err != nil {
			log.Println(err)
			continue
		}

		detail.LastSeen = lastSeen
		objectDetails = append(objectDetails, detail)
	}

	return objectDetails, nil
}

func deleteDetail(ctx context.Context, id string) error {
	query := "delete from %s where id = $1"

	if _, err := db.ExecContext(ctx, fmt.Sprintf(query, dbname), id); err != nil {
		return err
	}
	return nil
}

// fetchDetail calls localhost:9010/objects/id to receive details of an object by its id
func fetchDetail(objectID int) (ObjectDetail, error) {
	//host.docker.internal resolves to the internal IP address for host machine [source:docs.docker.com/docker-for-mac/networking/]
	//specifying this and not localhost so we can reach the the process running on localhost:9010
	
	endpoint := "http://host.docker.internal:9010/objects/"

	resp, err := http.Post(endpoint+fmt.Sprintf("%d", objectID), "text/html", nil)
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

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
