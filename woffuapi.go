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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyText, _ := io.ReadAll(resp.Body)
		log.Printf("getToken failed with status %d: %s", resp.StatusCode, string(bodyText))
		return "", errors.New("authentication failed with status " + strconv.Itoa(resp.StatusCode))
	}
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyText, _ := io.ReadAll(resp.Body)
		log.Printf("getUserID failed with status %d: %s", resp.StatusCode, string(bodyText))
		return "", errors.New("failed to get user ID with status " + strconv.Itoa(resp.StatusCode))
	}
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
	dateTime := getTodayDateString()
	requestURL := "https://" + w.Corp + ".woffu.com/api/users/" + w.WoffuUID + "/events?fromDate=" + dateTime
	req, err := http.NewRequest("GET", requestURL, nil)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyText, _ := io.ReadAll(resp.Body)
		log.Printf("getEvents failed with status %d: %s", resp.StatusCode, string(bodyText))
		return nil, errors.New("failed to get events with status " + strconv.Itoa(resp.StatusCode))
	}
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	events := []eventJSON{}
	if err := json.Unmarshal(bodyText, &events); err != nil {
		log.Println("Error parsing JSON in getEvents. Body:", string(bodyText))
		return nil, err
	}
	log.Printf("DEBUG getEvents: Parsed %d events from API", len(events))
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
		return errors.New("bad response")
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

func getTodayDateString() string {
	return time.Now().Format("2006-01-02")
}

type signDetail struct {
	Time string `json:"shortTrueTime"`
}

type signSlot struct {
	In  *signDetail `json:"in"`  // Pointer so we can check for nil
	Out *signDetail `json:"out"` // Pointer so we can check for nil
}

// isCheckedIn returns true if the user is currently checked in
func (w *woffu) isCheckedIn() (bool, error) {
	today := getTodayDateString()
	// Use the slots endpoint to get daily activity
	requestURL := "https://" + w.Corp + ".woffu.com/api/svc/signs/v2/signs/slots?fromDate=" + today + "&toDate=" + today
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return false, err
	}
	addCommonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+w.WoffuToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://"+w.Corp+".woffu.com")
	req.Header.Set("Referer", "https://"+w.Corp+".woffu.com/v2/personal/dashboard/user")

	resp, err := w.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyText, _ := io.ReadAll(resp.Body)
		log.Printf("isCheckedIn failed with status %d: %s", resp.StatusCode, string(bodyText))
		return false, errors.New("failed to check sign status with status " + strconv.Itoa(resp.StatusCode))
	}
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	// Parse as slots format
	slots := []signSlot{}
	if err := json.Unmarshal(bodyText, &slots); err != nil {
		log.Println("Error parsing JSON in isCheckedIn. Body:", string(bodyText))
		return false, err
	}

	log.Printf("DEBUG isCheckedIn: Parsed %d slots from API", len(slots))

	// If no slots today, user is not checked in
	if len(slots) == 0 {
		log.Println("DEBUG isCheckedIn: No slots found -> User is NOT checked in")
		return false, nil
	}

	// Check the last slot
	lastSlot := slots[len(slots)-1]

	// Log the slot details
	inTime := "nil"
	outTime := "nil"
	if lastSlot.In != nil {
		inTime = lastSlot.In.Time
	}
	if lastSlot.Out != nil {
		outTime = lastSlot.Out.Time
	}
	log.Printf("DEBUG isCheckedIn: Last slot - In: %s, Out: %s", inTime, outTime)

	// User is checked in if Out is nil (no checkout time)
	if lastSlot.Out == nil {
		log.Println("DEBUG isCheckedIn: Last slot has no Out time -> User IS checked in")
		return true, nil
	}

	log.Println("DEBUG isCheckedIn: Last slot has Out time -> User is NOT checked in")
	return false, nil
}
