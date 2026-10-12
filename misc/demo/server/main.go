// Command server is the sign-up service the demo in misc/demo runs Probe
// against: an HTTP API that keeps its users in a SQLite file, so that a
// workflow can call an endpoint and then query the row it wrote.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"

	_ "modernc.org/sqlite"
)

type user struct {
	ID     int64  `json:"id"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	path := flag.String("db", "app.db", "SQLite file to keep the users in")
	flag.Parse()

	db, err := sql.Open("sqlite", *path)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	const schema = `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		email TEXT NOT NULL,
		status TEXT NOT NULL
	)`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Email == "" {
			http.Error(w, "email is required", http.StatusBadRequest)
			return
		}
		res, err := db.Exec("INSERT INTO users (email, status) VALUES (?, 'pending')", in.Email)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()
		reply(w, http.StatusCreated, user{ID: id, Email: in.Email, Status: "pending"})
	})
	mux.HandleFunc("POST /users/{id}/activate", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := db.Exec("UPDATE users SET status = 'active' WHERE id = ?", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		show(w, db, id)
	})
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		show(w, db, r.PathValue("id"))
	})

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func show(w http.ResponseWriter, db *sql.DB, id string) {
	var u user
	err := db.QueryRow("SELECT id, email, status FROM users WHERE id = ?", id).Scan(&u.ID, &u.Email, &u.Status)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "no such user", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reply(w, http.StatusOK, u)
}

func reply(w http.ResponseWriter, code int, u user) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(u)
}
