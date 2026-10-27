// A stub of a sign-up service for the hero workflow of the docs home page:
// a web form, a JSON API, SQLite, and a confirmation mail sent over SMTP.
package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/smtp"

	_ "modernc.org/sqlite"
)

var (
	addr   = flag.String("addr", "127.0.0.1:8080", "listen address")
	dbPath = flag.String("db", "app.db", "SQLite file")
	smtpAt = flag.String("smtp", "127.0.0.1:3025", "SMTP server")
	base   = flag.String("base", "http://127.0.0.1:8080", "base URL of the links in mail")
)

func page(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<!doctype html><html><head><title>Acme</title></head><body><main>%s</main></body></html>", body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func token() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func main() {
	flag.Parse()
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY, email TEXT UNIQUE, password TEXT, status TEXT, token TEXT, name TEXT DEFAULT '')`); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("GET /signup", func(w http.ResponseWriter, r *http.Request) {
		page(w, 200, `<h1>Create your account</h1>
<form method="post" action="/signup">
<label>Email <input id="email" name="email" type="email"></label>
<label>Password <input id="password" name="password" type="password"></label>
<button type="submit">Sign up</button>
</form>`)
	})

	http.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
		email, password := r.FormValue("email"), r.FormValue("password")
		tok := token()
		if _, err := db.Exec(`INSERT INTO users (email, password, status, token) VALUES (?, ?, 'pending', ?)`,
			email, password, tok); err != nil {
			page(w, 409, "<h1>That address is taken</h1>")
			return
		}
		msg := fmt.Sprintf("From: Acme <no-reply@example.com>\r\nTo: %s\r\nSubject: Confirm your address\r\n\r\nFollow the link to confirm your address:\r\n%s/confirm?token=%s\r\n",
			email, *base, tok)
		if err := smtp.SendMail(*smtpAt, nil, "no-reply@example.com", []string{email}, []byte(msg)); err != nil {
			log.Print(err)
		}
		page(w, 201, fmt.Sprintf(`<h1>Almost there</h1><p id="notice">Check your inbox to confirm %s.</p>`, html.EscapeString(email)))
	})

	http.HandleFunc("GET /confirm", func(w http.ResponseWriter, r *http.Request) {
		res, _ := db.Exec(`UPDATE users SET status = 'active' WHERE token = ? AND status = 'pending'`, r.URL.Query().Get("token"))
		if n, _ := res.RowsAffected(); n != 1 {
			page(w, 404, "<h1>This link has expired</h1>")
			return
		}
		page(w, 200, "<h1>Your account is active</h1>")
	})

	http.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Email, Password string }
		json.NewDecoder(r.Body).Decode(&in)
		var status string
		err := db.QueryRow(`SELECT status FROM users WHERE email = ? AND password = ?`, in.Email, in.Password).Scan(&status)
		switch {
		case err != nil:
			writeJSON(w, 401, map[string]string{"error": "wrong email or password"})
		case status != "active":
			writeJSON(w, 403, map[string]string{"error": "confirm your address first"})
		default:
			writeJSON(w, 200, map[string]string{"token": token(), "email": in.Email})
		}
	})

	// The web session: the cookie holds the email, as a stub may.
	user := func(r *http.Request) string {
		c, err := r.Cookie("session")
		if err != nil {
			return ""
		}
		return c.Value
	}

	http.HandleFunc("GET /signin", func(w http.ResponseWriter, r *http.Request) {
		page(w, 200, `<h1>Sign in</h1>
<form method="post" action="/signin">
<label>Email <input name="email" type="email"></label>
<label>Password <input name="password" type="password"></label>
<button type="submit">Sign in</button>
</form>`)
	})

	http.HandleFunc("POST /signin", func(w http.ResponseWriter, r *http.Request) {
		email := r.FormValue("email")
		var status string
		err := db.QueryRow(`SELECT status FROM users WHERE email = ? AND password = ?`, email, r.FormValue("password")).Scan(&status)
		if err != nil || status != "active" {
			page(w, 401, `<h1>Sign in</h1><p>Wrong email or password. <a href="/signin">Try again</a></p>`)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: email, Path: "/"})
		http.Redirect(w, r, "/home", http.StatusSeeOther)
	})

	http.HandleFunc("GET /home", func(w http.ResponseWriter, r *http.Request) {
		if user(r) == "" {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		page(w, 200, `<nav><a href="/home">Home</a> · <a href="/account">Account</a></nav><h1>Your projects</h1><p>You have no projects yet.</p>`)
	})

	http.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		email := user(r)
		if email == "" {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		var name string
		db.QueryRow(`SELECT name FROM users WHERE email = ?`, email).Scan(&name)
		saved := ""
		if r.URL.Query().Get("saved") == "1" {
			saved = `<p role="status">Saved.</p>`
		}
		page(w, 200, fmt.Sprintf(`<nav><a href="/home">Home</a> · <a href="/account">Account</a></nav><h1>Account</h1>%s
<p>Email: %s</p>
<form method="post" action="/account">
<label>Display name <input name="name" value="%s"></label>
<button type="submit">Save</button>
</form>`, saved, html.EscapeString(email), html.EscapeString(name)))
	})

	http.HandleFunc("POST /account", func(w http.ResponseWriter, r *http.Request) {
		email := user(r)
		if email == "" {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		db.Exec(`UPDATE users SET name = ? WHERE email = ?`, r.FormValue("name"), email)
		http.Redirect(w, r, "/account?saved=1", http.StatusSeeOther)
	})

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
