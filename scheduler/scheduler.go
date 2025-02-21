package scheduler

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/artem-streltsov/ucl-timetable-bot/database"
	"github.com/artem-streltsov/ucl-timetable-bot/timetable"
	"github.com/artem-streltsov/ucl-timetable-bot/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var ukLocation, _ = time.LoadLocation("Europe/London")

type Scheduler struct {
	api    *tgbotapi.BotAPI
	db     *database.DB
	timers map[int64]*UserTimers
}

type UserTimers struct {
	dailyTimer       *time.Timer
	weeklyTimer      *time.Timer
	lectureTimers    []*time.Timer
	lectureScheduler *time.Timer
}

func NewScheduler(api *tgbotapi.BotAPI, db *database.DB) *Scheduler {
	return &Scheduler{
		api:    api,
		db:     db,
		timers: make(map[int64]*UserTimers),
	}
}

func (s *Scheduler) ScheduleAll() {
	users, _ := s.db.GetAllUsers()
	for _, user := range users {
		s.ScheduleUser(user.ChatID)
	}
}

func (s *Scheduler) ScheduleUser(chatID int64) {
	user, err := s.db.GetUser(chatID)
	if err != nil || user == nil {
		return
	}

	s.CancelUser(chatID)

	s.timers[chatID] = &UserTimers{}

	dailyTime := utils.GetNextTime(user.DailyTime)
	dailyDuration := time.Until(dailyTime)
	dailyTimer := time.AfterFunc(dailyDuration, func() {
		s.sendDailyTimetable(chatID)
		s.ScheduleUser(chatID)
	})
	s.timers[chatID].dailyTimer = dailyTimer

	weeklyTime := utils.GetNextWeekTime(user.WeeklyTime)
	weeklyDuration := time.Until(weeklyTime)
	weeklyTimer := time.AfterFunc(weeklyDuration, func() {
		s.sendWeeklyTimetable(chatID)
		s.ScheduleUser(chatID)
	})
	s.timers[chatID].weeklyTimer = weeklyTimer

	s.scheduleLectureRemindersAtMidnight(chatID)
}

func (s *Scheduler) scheduleLectureRemindersAtMidnight(chatID int64) {
	now := time.Now().In(ukLocation)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 1, 0, ukLocation).AddDate(0, 0, 1)
	durationUntilMidnight := time.Until(midnight)

	lectureScheduler := time.AfterFunc(durationUntilMidnight, func() {
		s.scheduleLectureReminders(chatID)
		s.scheduleLectureRemindersAtMidnight(chatID)
	})
	s.timers[chatID].lectureScheduler = lectureScheduler

	s.scheduleLectureReminders(chatID)
}

func (s *Scheduler) scheduleLectureReminders(chatID int64) {
	if s.timers[chatID] != nil {
		for _, timer := range s.timers[chatID].lectureTimers {
			timer.Stop()
		}
		s.timers[chatID].lectureTimers = nil
	}

	user, _ := s.db.GetUser(chatID)
	if user == nil || user.WebCalURL == "" {
		return
	}

	cal, err := timetable.FetchCalendar(user.WebCalURL)
	if err != nil {
		return
	}

	day := time.Now().In(ukLocation)
	lectures, err := timetable.GetLectures(cal, day)
	if err != nil || len(lectures) == 0 {
		return
	}

	offsetMinutes, err := strconv.Atoi(user.ReminderOffset)
	if err != nil {
		offsetMinutes = 15
	}

	timers := []*time.Timer{}
	now := time.Now().In(ukLocation)

	for _, lecture := range lectures {
		reminderTime := lecture.Start.Add(-time.Duration(offsetMinutes) * time.Minute)
		if reminderTime.After(now) {
			duration := time.Until(reminderTime)
			lectureCopy := lecture
			timer := time.AfterFunc(duration, func() {
				reminderMessage := fmt.Sprintf("⏰ *%s* in %d minutes at %s",
					timetable.CleanTitle(lectureCopy.Title),
					offsetMinutes,
					lectureCopy.Location,
				)
				s.sendMessage(chatID, reminderMessage)
			})
			timers = append(timers, timer)
		}
	}
	s.timers[chatID].lectureTimers = timers
}

func (s *Scheduler) sendDailyTimetable(chatID int64) {
	user, _ := s.db.GetUser(chatID)
	if user == nil || user.WebCalURL == "" {
		s.sendMessage(chatID, "Please set your calendar link using /set_calendar")
		return
	}
	cal, err := timetable.FetchCalendar(user.WebCalURL)
	if err != nil {
		s.sendMessage(chatID, "Error fetching calendar: "+err.Error())
		return
	}

	day := time.Now().In(ukLocation)
	lectures, err := timetable.GetLectures(cal, day)
	if err != nil {
		s.sendMessage(chatID, "Error processing calendar: "+err.Error())
		return
	}
	if len(lectures) == 0 {
		s.sendMessage(chatID, "No lectures today.")
		return
	}
	dateStr := day.Format("Mon, 02 Jan")
	message := fmt.Sprintf("*%s:*\n\n", dateStr) + timetable.FormatLectures(lectures)
	s.sendMessage(chatID, message)
}

func (s *Scheduler) sendWeeklyTimetable(chatID int64) {
	user, _ := s.db.GetUser(chatID)
	if user == nil || user.WebCalURL == "" {
		s.sendMessage(chatID, "Please set your calendar link using /set_calendar")
		return
	}
	cal, err := timetable.FetchCalendar(user.WebCalURL)
	if err != nil {
		s.sendMessage(chatID, "Error fetching calendar: "+err.Error())
		return
	}

	now := time.Now().In(ukLocation)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // make Sunday 7
	}
	weekStart := now.AddDate(0, 0, -(weekday - 1)) // Monday
	weekEnd := weekStart.AddDate(0, 0, 4)          // Friday

	lecturesMap, err := timetable.GetLecturesInRange(cal, weekStart, weekEnd)
	if err != nil {
		s.sendMessage(chatID, "Error processing calendar: "+err.Error())
		return
	}
	if len(lecturesMap) == 0 {
		s.sendMessage(chatID, "No lectures this week.")
		return
	}
	startDateStr := weekStart.Format("Mon, 02 Jan")
	endDateStr := weekEnd.Format("Fri, 02 Jan")
	dateRangeStr := fmt.Sprintf("*%s - %s:*\n\n", startDateStr, endDateStr)

	var sb strings.Builder
	sb.WriteString(dateRangeStr)
	for day := weekStart; !day.After(weekEnd); day = day.AddDate(0, 0, 1) {
		dayKey := day.Format("Monday")
		lectures, ok := lecturesMap[dayKey]
		if ok {
			sb.WriteString("\n" + "*" + dayKey + "*" + "\n")
			message := timetable.FormatLectures(lectures)
			sb.WriteString(message)
		}
	}
	s.sendMessage(chatID, sb.String())
}

func (s *Scheduler) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := s.api.Send(msg); err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

func (s *Scheduler) CancelUser(chatID int64) {
	if timers, exists := s.timers[chatID]; exists {
		if timers.dailyTimer != nil {
			timers.dailyTimer.Stop()
		}
		if timers.weeklyTimer != nil {
			timers.weeklyTimer.Stop()
		}
		if timers.lectureScheduler != nil {
			timers.lectureScheduler.Stop()
		}
		for _, timer := range timers.lectureTimers {
			timer.Stop()
		}
		delete(s.timers, chatID)
	}
}

func (s *Scheduler) StopAll() {
	for chatID := range s.timers {
		s.CancelUser(chatID)
	}
}
