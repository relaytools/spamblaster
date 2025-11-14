package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"github.com/nbd-wtf/go-nostr"
)

var jar, _ = cookiejar.New(nil)

var client http.Client

func signEventWithLoginToken(baseURL string, privateKey string) nostr.Event {
	req, err := http.NewRequest("GET", baseURL+"/api/auth/logintoken", nil)
	if err != nil {
		log(fmt.Sprintf("Got error %s", err.Error()))
	}
	resp, err := client.Do(req)
	if err != nil {
		log(fmt.Sprintf("Error occured. Error is: %s", err.Error()))
	}
	defer resp.Body.Close()
	var data map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &data)

	//fmt.Println(data["token"])
	pub, _ := nostr.GetPublicKey(privateKey)

	// create event to sign
	ev := nostr.Event{
		PubKey:    pub,
		CreatedAt: nostr.Now(),
		Kind:      27235,
		Tags:      nil,
		Content:   fmt.Sprint(data["token"]),
	}
	ev.Sign(privateKey)
	return ev
}

func getCSRF(baseURL string) string {
	req, err := http.NewRequest("GET", baseURL+"/api/auth/csrf", nil)
	if err != nil {
		log(fmt.Sprintf("Got error %s", err.Error()))
	}
	resp, err := client.Do(req)
	if err != nil {
		log(fmt.Sprintf("Error occured. Error is: %s", err.Error()))
	}
	defer resp.Body.Close()
	var csrfData map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &csrfData)
	return fmt.Sprint(csrfData["csrfToken"])
}

func performLogin(baseURL string, ev nostr.Event, csrf string) {
	form := url.Values{}
	form.Set("kind", fmt.Sprint(ev.Kind))
	form.Set("content", ev.Content)
	form.Set("created_at", fmt.Sprint(ev.CreatedAt))
	form.Set("pubkey", ev.PubKey)
	form.Set("sig", ev.Sig)
	form.Set("id", ev.ID)
	form.Set("csrfToken", csrf)
	form.Set("callbackUrl", baseURL)
	form.Set("json", "true")

	req, err := http.NewRequest("POST", baseURL+"/api/auth/callback/credentials", bytes.NewBufferString(form.Encode()))
	if err != nil {
		log(fmt.Sprintf("Error occurred while creating request. Error is: %s", err.Error()))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		log(fmt.Sprintf("Error occurred while making request. Error is: %s", err.Error()))
	}
	defer resp.Body.Close()
}

func init() {
	client = http.Client{
		Jar: jar,
	}
}
