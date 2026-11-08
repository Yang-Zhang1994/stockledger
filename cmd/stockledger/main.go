// Command stockledger runs the inventory service.
//
//	go run ./cmd/stockledger -mem                  # no dependencies
//	docker compose up -d && go run ./cmd/stockledger   # MySQL + Kafka
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Yang-Zhang1994/stockledger/internal/api"
	"github.com/Yang-Zhang1994/stockledger/internal/eventstore"
	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
	"github.com/Yang-Zhang1994/stockledger/internal/projection"
	"github.com/Yang-Zhang1994/stockledger/internal/relay"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	mem := flag.Bool("mem", false, "use an in-memory store and skip Kafka")
	addr := flag.String("addr", env("ADDR", ":8080"), "listen address")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	read := projection.NewAvailable()
	var store eventstore.Store
	var pub relay.Publisher

	if *mem {
		store = eventstore.NewMemory()
		pub = relay.LocalPublisher(read.Handle)
	} else {
		db, err := sql.Open("mysql", env("MYSQL_DSN", "stockledger:stockledger@tcp(127.0.0.1:3307)/stockledger?parseTime=true"))
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
		if err := db.PingContext(ctx); err != nil {
			log.Fatalf("mysql: %v", err)
		}
		store = eventstore.NewMySQL(db)

		brokers := strings.Split(env("KAFKA_BROKERS", "127.0.0.1:9092"), ",")
		topic := env("KAFKA_TOPIC", "inventory-events")
		kp := relay.NewKafkaPublisher(brokers, topic)
		defer kp.Close()
		pub = kp
		go func() {
			err := relay.Consume(ctx, brokers, topic, "available-projection", read.Handle)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("consumer stopped: %v", err)
			}
		}()
	}

	go relay.New(store, pub).Run(ctx, 500*time.Millisecond)

	srv := &http.Server{Addr: *addr, Handler: api.New(inventory.NewService(store), store, read), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("stockledger listening on %s (mem=%v)", *addr, *mem)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
