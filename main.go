package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	telegram "github.com/go-telegram-bot-api/telegram-bot-api"
)

const (
	ReconcileIntervalMinutes = 7
)

type woffu struct {
	User            string
	Pass            string
	Corp            string
	BotToken        string
	ChatID          int64
	CheckInHour     int
	CheckInMinute   int
	CheckOutHour    int
	CheckOutMinute  int
	WorkingEventIDs []int
	Bot             *telegram.BotAPI
	WoffuToken      string
	WoffuUID        string
	SkipList        []string
	Client          *http.Client
}

func main() {
	// Setup logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Woffu Bot...")

	// Handle signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-c
		log.Printf("Received termination signal: %v. Shutting down...", sig)
		os.Exit(0)
	}()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL ERROR (PANIC): %v", r)
			os.Exit(1)
		}
	}()

	// Load config
	w, err := newBot()
	if err != nil {
		log.Fatalf("FATAL: Cannot start bot due to configuration error: %v", err)
	}

	// Get credentials
	err = w.login()
	if err != nil {
		log.Printf("Error during initial login: %v. Will retry in reconciliation loop.", err)
		if w.Bot != nil {
			w.sendError(errors.New("Initial login failed: " + err.Error() + ". Will retry."))
		}
		// Don't panic - let the reconciliation loop handle retries
	} else {
		log.Println("Initial login successful")
	}

	log.Println("Bot initialized successfully. Starting reconciliation loop with 30-minute polling interval.")

	// Perform immediate reconciliation check on startup
	w.reconcile()

	// Create a ticker for reconciliation intervals
	ticker := time.NewTicker(ReconcileIntervalMinutes * time.Minute)
	defer ticker.Stop()

	// Endless loop with ticker-based polling
	for range ticker.C {
		w.reconcile()
	}
}

func (w *woffu) reconcile() {
	log.Println("=== Starting reconciliation check ===")

	// Check if today is a working day
	evs, err := w.getEvents()
	if err != nil {
		log.Printf("Error getting events during reconciliation: %v", err)
		w.sendError(errors.New("Failed to get events: " + err.Error()))
		// Try to re-login for next attempt
		if loginErr := w.login(); loginErr != nil {
			log.Printf("Error re-login during reconciliation: %v", loginErr)
		}
		return
	}

	// DEBUG: Log configured working day IDs
	log.Printf("DEBUG: Configured Working Day IDs: %v", w.WorkingEventIDs)

	// DEBUG: Log what Woffu API returned
	if len(evs) == 0 {
		log.Println("DEBUG: Woffu returned NO events for today")
	} else {
		log.Printf("DEBUG: Woffu API returned Event: ID=%d, Name=\"%s\"", evs[0].ID, evs[0].Name)
	}

	// Determine if it's a working day
	isWorkingDay := false
	weekday := time.Now().Weekday()

	// Step A: Check if it's a weekend
	if weekday == time.Saturday || weekday == time.Sunday {
		log.Printf("DEBUG: Today is %s (weekend) -> Non-Working Day", weekday)
		isWorkingDay = false
	} else {
		// Step B: It's a weekday (Mon-Fri)
		log.Printf("DEBUG: Today is %s (weekday)", weekday)

		if len(evs) == 0 {
			// Empty list on a weekday = Standard working day
			log.Println("DEBUG: No events found on weekday -> Standard Working Day")
			isWorkingDay = true
		} else {
			// Events exist - check if any match the configured working day IDs
			// If they match, it's a working day. If not, it's a holiday/vacation.
			for _, id := range w.WorkingEventIDs {
				match := evs[0].ID == id
				log.Printf("DEBUG: Comparing EventID [%d] with ConfiguredID [%d]... Match? %v", evs[0].ID, id, match)
				if match {
					isWorkingDay = true
					log.Printf("DEBUG: ID %d MATCHES configured working ID. Treating as Working Day.", id)
					break
				}
			}
			if !isWorkingDay {
				log.Printf("DEBUG: Event ID %d does NOT match any configured working ID. Treating as Non-Working Day (Holiday/Vacation).", evs[0].ID)
			}
		}
	}

	// Check skip list
	isSkipDay := false
	today := getCurrentDate()
	for i, skipDate := range w.SkipList {
		if skipDate == today {
			isSkipDay = true
			w.SkipList[i] = w.SkipList[len(w.SkipList)-1]
			w.SkipList = w.SkipList[:len(w.SkipList)-1]
			log.Println("Today is in skip list, will not check in/out")
			break
		}
	}

	if !isWorkingDay || isSkipDay {
		if isSkipDay {
			log.Println("Skip day - no action needed")
		} else {
			log.Println("Not a working day - no action needed")
		}
		return
	}

	log.Println("Today is a working day. Checking current time and user status...")

	// Get current time
	now := time.Now()
	currentHour := now.Hour()
	currentMinute := now.Minute()

	// Convert times to minutes for easier comparison
	currentTimeInMinutes := currentHour*60 + currentMinute
	workStartInMinutes := w.CheckInHour*60 + w.CheckInMinute
	workEndInMinutes := w.CheckOutHour*60 + w.CheckOutMinute

	isWithinWorkingHours := currentTimeInMinutes >= workStartInMinutes && currentTimeInMinutes < workEndInMinutes

	log.Printf("Current time: %02d:%02d, Within working hours (%02d:%02d-%02d:%02d): %v", currentHour, currentMinute, w.CheckInHour, w.CheckInMinute, w.CheckOutHour, w.CheckOutMinute, isWithinWorkingHours)

	// Check user's current status
	checkedIn, err := w.isCheckedIn()
	if err != nil {
		log.Printf("Error checking user status: %v", err)
		w.sendError(errors.New("Failed to check user status: " + err.Error()))
		// Try to re-login for next attempt
		if loginErr := w.login(); loginErr != nil {
			log.Printf("Error re-login after status check failure: %v", loginErr)
		}
		return
	}

	log.Printf("User is currently checked in: %v", checkedIn)

	// Decision matrix
	if isWithinWorkingHours && !checkedIn {
		// Need to check in
		log.Println("ACTION REQUIRED: User should be checked in but is not. Performing check-in...")
		if err := w.check(); err != nil {
			log.Printf("Error performing check-in: %v", err)
			w.sendError(errors.New("Check-in failed: " + err.Error()))
		} else {
			log.Println("Check-in successful")
			w.sendMessage("✅ Checked in successfully")
		}
	} else if !isWithinWorkingHours && checkedIn {
		// Need to check out
		log.Println("ACTION REQUIRED: User should be checked out but is still checked in. Performing check-out...")
		if err := w.check(); err != nil {
			log.Printf("Error performing check-out: %v", err)
			w.sendError(errors.New("Check-out failed: " + err.Error()))
		} else {
			log.Println("Check-out successful")
			w.sendMessage("✅ Checked out successfully")
		}
	} else {
		// State is correct
		if isWithinWorkingHours && checkedIn {
			log.Println("State is correct: Within working hours and checked in ✓")
		} else if !isWithinWorkingHours && !checkedIn {
			log.Println("State is correct: Outside working hours and checked out ✓")
		}
	}

	log.Println("=== Reconciliation check complete ===")
}

func newBot() (*woffu, error) {
	// Load config
	w, err := loadConfig()
	if err != nil {
		return nil, err
	}

	// Run bot
	err = w.runTelegramBot()
	return w, err
}

func loadConfig() (*woffu, error) {
	user := os.Getenv("WOFFU_USER")
	if user == "" {
		return nil, errors.New("WOFFU_USER env value is mandatory")
	}
	pass := os.Getenv("WOFFU_PASS")
	if pass == "" {
		return nil, errors.New("WOFFU_PASS env value is mandatory")
	}
	corp := os.Getenv("CORP")
	if corp == "" {
		return nil, errors.New("CORP env value is mandatory")
	}
	botToken := os.Getenv("BOT")
	chatID, err := strconv.Atoi(os.Getenv("CHAT"))
	if botToken != "" && err != nil {
		return nil, err
	}
	parseTime := func(s string) (int, int, error) {
		splitted := strings.Split(s, ":")
		if len(splitted) != 2 {
			return 0, 0, errors.New("invalid time format, expected HH:MM")
		}
		hour, err := strconv.Atoi(splitted[0])
		if err != nil {
			return 0, 0, err
		}
		minute, err := strconv.Atoi(splitted[1])
		if err != nil {
			return 0, 0, err
		}
		if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return 0, 0, errors.New("wrong value")
		}
		return hour, minute, nil
	}
	checkInHour, checkInMinute, err := parseTime(os.Getenv("CHECKIN"))
	if err != nil {
		return nil, errors.New("Error parsing CHECKIN: " + err.Error())
	}
	checkOutHour, checkOutMinute, err := parseTime(os.Getenv("CHECKOUT"))
	if err != nil {
		return nil, errors.New("Error parsing CHECKOUT: " + err.Error())
	}
	splitted := strings.Split(os.Getenv("WORKINGDAYIDS"), ",")
	workingIDs := []int{}
	for _, idStr := range splitted {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return nil, err
		}
		workingIDs = append(workingIDs, id)
	}
	return &woffu{
		User:            user,
		Pass:            pass,
		Corp:            corp,
		BotToken:        botToken,
		ChatID:          int64(chatID),
		CheckInHour:     checkInHour,
		CheckInMinute:   checkInMinute,
		CheckOutHour:    checkOutHour,
		CheckOutMinute:  checkOutMinute,
		WorkingEventIDs: workingIDs,
		SkipList:        []string{},
		Client:          &http.Client{Timeout: 30 * time.Second},
	}, nil
}
