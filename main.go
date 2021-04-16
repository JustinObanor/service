package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

// ObjectList holds the list of object ids
type ObjectList struct {
	ObjectIDs []int `json:"object_ids"`
}

// ObjectDetail holds the status of a single ID stored in postgres
type ObjectDetail struct {
	ID       int  `json:"id"`
	Online   bool `json:"online"`
	LastSeen time.Time
}

// result is a ds that holds possible errors and/or object details using a channel while fetching the detail of an object
type result struct {
	objectDetail ObjectDetail
	err          error
}

var (
	db       *sql.DB
	host     = getenv("PSQL_HOST", "objects")
	port     = getenv("PSQL_PORT", "5432")
	user     = getenv("PSQL_USER", "postgres")
	password = getenv("PSQL_PWDcas", "12345")
	dbname   = getenv("PSQL_DB_NAME", "objects")
)

func newObjectList() *ObjectList {
	return &ObjectList{
		ObjectIDs: make([]int, 0, 200),
	}
}

func init() {
	var err error
	db, err = sql.Open("postgres", fmt.Sprintf(`host=%s port=%s user=%s
		password=%s dbname=%s sslmode=disable`,
		host, port, user, password, dbname))
	if err != nil {
		log.Fatal("error opening connection to postgres: ", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal("error pinging postgres client: ", err)
	}
	log.Printf("connected to psql client, host: %s\n", host)
}

func main() {
	callbackAddr := flag.String("callback", ":9090", "http listen address for callbacks body")

	flag.Parse()

	errChan := make(chan error)

	//handle shutdown signals
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		errChan <- fmt.Errorf("%s", <-sig)
	}()

	objList := newObjectList()
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// receive object ids from /callback path
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			log.Println("nil request body")
			http.Error(w, "no request body found", http.StatusBadRequest)
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&objList); err != nil {
			log.Println("error decoding request")
			http.Error(w, "error decoding request", http.StatusBadRequest)
			return
		}

		log.Println("sent ids length: ", len(objList.ObjectIDs))
		ready <- struct{}{}
	})

	resChan := make(chan result)
	//goroutine that blocks until a new object id list has been received or context cancelled
	go func() {
		for {
			select {
			case <-ctx.Done():
				errChan <- errors.New("context cancelled")
				return

			case <-ready:
				log.Println("received ids length: ", len(objList.ObjectIDs))

				for _, id := range objList.ObjectIDs {
					go func(objectID int) {
						details, err := fetchDetail(objectID)
						if err != nil {
							resChan <- result{err: err}
						}

						resChan <- result{objectDetail: details}
					}(id)
				}
			}
		}
	}()

	online := make([]ObjectDetail, 0)
	offline := make([]ObjectDetail, 0)

	statusChan := make(chan struct{})
	pullDbData := make(chan struct{})
	//goroutine that blocks until it receives details or possibly an error the the fetched detail or context cancelled
	go func() {
		for {
			select {
			case <-ctx.Done():
				errChan <- errors.New("context cancelled")
				return

			case res := <-resChan:
				if res.err != nil {
					continue
				}
				if res.objectDetail.Online {
					online = append(online, res.objectDetail)
				} else {
					offline = append(offline, res.objectDetail)
				}
				statusChan <- struct{}{}

				if err := toStore(ctx, res.objectDetail); err != nil {
					log.Println(err)
					return
				}

				pullDbData <- struct{}{}
			}
		}
	}()

	go func() {
		for {
			<-pullDbData
			details, err := fromStore(ctx)
			if err != nil {
				cancel()
			}

			for _, detail := range details {
				if time.Since(detail.LastSeen) > time.Second*30 {
					if err := deleteDetail(ctx, strconv.Itoa(detail.ID)); err != nil {
						log.Printf("error deleting detail, id %d: %v", detail.ID, err)
						return
					}
				}
			}

		}
	}()

	// access to object details filtered by online status
	go func() {
		for {
			<-statusChan
			//fmt.Println("online", len(online))
			//fmt.Println("offline", len(offline))
		}
	}()

	//listening for callback
	go func() {
		log.Printf("listening on port %s for callback\n", *callbackAddr)
		if err := http.ListenAndServe(*callbackAddr, nil); err != nil {
			errChan <- err
			cancel()
		}
	}()

	log.Println("exit: ", <-errChan)
}
