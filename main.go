package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var (
	mx       sync.RWMutex
	store    = make(map[string]*websocket.Conn)
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

func main() {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, "Failed to make connection", http.StatusInternalServerError)
			return
		}

		connID := uuid.New().String()

		addClient(connID, conn)
		defer removeClient(connID)

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("client [%s] disconnected: failed to read message: %v", connID, err)
				break
			}

			go writeToSubs(r.Context(), msg)
		}

	})

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	srv := http.Server{
		Addr: ":8080",
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("failed to start server: %v", err)
		}
	}()

	log.Println("server listening...")

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("error while closing server: %v", err)
	}

	closeAllClients()
}

func writeToSubs(ctx context.Context, msg []byte) {
	mx.RLock()
	defer mx.RUnlock()

	select {
	case <-ctx.Done():
		return
	default:
		for id, conn := range store {
			go func(id string, conn *websocket.Conn) {
				err := conn.WriteMessage(websocket.TextMessage, msg)
				if err != nil {
					removeClient(id)
					log.Printf("client [%s] disconnected: %v", id, err)
				}
			}(id, conn)
		}
	}

}

func addClient(id string, conn *websocket.Conn) {
	mx.Lock()
	defer mx.Unlock()

	store[id] = conn

	log.Printf("client [%s] connected and saved", id)

}

func removeClient(id string) {
	mx.Lock()
	defer mx.Unlock()

	if conn, ok := store[id]; ok {
		conn.Close()
		delete(store, id)

		log.Printf("client [%s] removed", id)
	}
}

func closeAllClients() {
	mx.Lock()
	defer mx.Unlock()

	for id, conn := range store {
		conn.Close()
		delete(store, id)
		log.Printf("Клиент [%s] закрыт и удален", id)
	}
}
