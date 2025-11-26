package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type tokenJSON struct {
	Token string `json:"access_token"`
}

func (w *woffu) getToken() (string, error) {
	var data = strings.NewReader("grant_type=password&username=" + w.User + "&password=" + w.Pass)
	req, err := http.NewRequest("POST", "https://app.woffu.com/token", data)
	if err != nil {
		return "", err
	}
	addCommonHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://app.woffu.com")
	req.Header.Set("Referer", "https://app.woffu.com/")
	resp, err := w.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	t := &tokenJSON{}
	if err := json.Unmarshal(bodyText, t); err != nil {
		log.Println("Error parsing JSON in getToken. Body:", string(bodyText))
		return "", err
	}
	return t.Token, nil
}

type userIDJSON struct {
	UserID int `json:"UserId"`
}

func (w *woffu) getUserID(token string) (string, error) {
	req, err := http.NewRequest("GET", "https://app.woffu.com/api/users", nil)
	if err != nil {
		return "", err
	}
	addCommonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Referer", "https://app.woffu.com/")
	resp, err := w.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	uid := &userIDJSON{}
	if err := json.Unmarshal(bodyText, uid); err != nil {
		log.Println("Error parsing JSON in getUserID. Body:", string(bodyText))
		return "", err
	}
	return strconv.Itoa(int(uid.UserID)), nil
}

func (w *woffu) login() error {
	// Get token
	token, err := w.getToken()
	if err != nil {
		return err
	}

	// Get user ID
	uid, err := w.getUserID(token)
	if err != nil {
		return err
	}
	w.WoffuToken = token
	w.WoffuUID = uid
	return nil
}

type eventJSON struct {
	ID   int    `json:"EventTypeId"`
	Name string `json:"Name"`
	Date string `json:"Date"`
}

func (w *woffu) getEvents() ([]eventJSON, error) {
	dateTime := getDate()
	req, err := http.NewRequest("GET", "https://"+w.Corp+".woffu.com/api/users/"+w.WoffuUID+"/events?fromDate="+dateTime, nil)
	if err != nil {
		return nil, err
	}
	addCommonHeaders(req)
	addAuthHeaders(req, w.Corp, w.WoffuToken)
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	events := []eventJSON{}
	if err := json.Unmarshal(bodyText, &events); err != nil {
		log.Println("Error parsing JSON in getEvents. Body:", string(bodyText))
		return nil, err
	}
	return events, nil
}

func (w *woffu) check() error {
	// Updated URL for Woffu API v2
	var data = strings.NewReader(`{"agreementEventId":null,"requestId":null,"deviceId":"WebApp","latitude":null,"longitude":null,"timezoneOffset":-60}`)
	req, err := http.NewRequest("POST", "https://"+w.Corp+".woffu.com/api/svc/signs/v2/signs/signrequest", data)
	if err != nil {
		return err
	}
	addCommonHeaders(req)
	// Use the token for authorization
	req.Header.Set("Authorization", "Bearer "+w.WoffuToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://"+w.Corp+".woffu.com")
	req.Header.Set("Referer", "https://"+w.Corp+".woffu.com/v2/personal/dashboard/user")

	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Println(resp.StatusCode)
		log.Println(string(bodyText))
		return errors.New("Bad response")
	}
	return nil
}

func addCommonHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:76.0) Gecko/20100101 Firefox/76.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("DNT", "1")
	req.Header.Set("TE", "Trailers")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Connection", "keep-alive")
}

func addAuthHeaders(req *http.Request, corp, token string) {
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://"+corp+".woffu.com")
	req.Header.Set("Referer", "https://"+corp+".woffu.com/")
	req.Header.Set("Cookie", "woffu.token="+token)
}

func getDate() string {
	return time.Now().Format(time.RFC3339)
}
