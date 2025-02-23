package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

const completeLoginPath = "/completeLogin"

type spotifyAuthHandler struct {
	auth *spotifyauth.Authenticator

	authState string
	port      int

	token *oauth2.Token
}

func spotifyClientFromToken(token *oauth2.Token) *spotify.Client {
	httpClient := spotifyauth.New().Client(context.Background(), token)
	return spotify.New(httpClient)
}

func newSpotifyAuthHander(port int) *spotifyAuthHandler {
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d%s", port, completeLoginPath)

	return &spotifyAuthHandler{
		auth: spotifyauth.New(
			spotifyauth.WithRedirectURL(redirectURI),
			spotifyauth.WithScopes(
				spotifyauth.ScopeUserReadCurrentlyPlaying,
				spotifyauth.ScopeUserReadPlaybackState,
				spotifyauth.ScopeUserModifyPlaybackState,
				spotifyauth.ScopePlaylistReadPrivate,
				spotifyauth.ScopePlaylistReadCollaborative,
			),
		),
		authState: uuid.New().String(),
		port:      port,
	}
}

func (h *spotifyAuthHandler) Token(ctx context.Context) *oauth2.Token {
	cancelSrv := make(chan bool)

	http.HandleFunc(completeLoginPath, func(w http.ResponseWriter, r *http.Request) {
		if st := r.FormValue("state"); st != h.authState {
			log.Printf("mismatched state value: got %s, expected %s\n", st, h.authState)
			http.NotFound(w, r)
			return
		}

		tok, err := h.auth.Token(r.Context(), h.authState, r)
		if err != nil {
			log.Printf("unable to get spotify token: %s\n", err.Error())
			http.Error(w, "Couldn't get token", http.StatusForbidden)
			return
		}

		h.token = tok
		log.Print("received spotify token\n")

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "Login Successful")

		cancelSrv <- true
	})

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", h.port),
	}

	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("unable to start spotify listener: %s\n", err.Error())
		}
	}()

	url := h.auth.AuthURL(h.authState)
	fmt.Printf("Please go to %s to log in\n", url)

	<-cancelSrv
	log.Printf("shutting down http listener and continuing\n")
	srv.Shutdown(ctx)

	if h.token == nil {
		log.Fatal("oauth token wasn't populated; can't start\n")
	}

	return h.token
}
