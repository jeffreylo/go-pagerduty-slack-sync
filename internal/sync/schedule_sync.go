package sync

import (
	"strings"
	"time"

	"github.com/kevholditch/go-pagerduty-slack-sync/internal/compare"
	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

// Schedules does the sync
func Schedules(config *Config) error {
	logrus.Infof("running schedule sync...")
	s, err := newSlackClient(config.SlackToken)
	if err != nil {
		return err
	}
	p := newPagerDutyClient(config.PagerDutyToken)

	updateSlackGroup := func(emails []string, groupName string) error {
		slackIDs, err := s.getSlackIDsFromEmails(emails)
		if err != nil {
			return err
		}

		userGroup, err := s.createOrGetUserGroup(groupName)
		if err != nil {
			return err
		}
		members, err := s.Client.GetUserGroupMembers(userGroup.ID)
		if err != nil {
			return err
		}

		if !compare.Array(slackIDs, members) {
			logrus.Infof("member list %s needs updating...", groupName)
			_, err = s.Client.UpdateUserGroupMembers(userGroup.ID, strings.Join(slackIDs, ","))
			if err != nil {
				return err
			}
		}
		return nil
	}

	getEmailsForSchedules := func(schedules []string, lookahead time.Duration) ([]string, error) {
		var emails []string

		for _, sid := range schedules {
			e, err := p.getEmailsForSchedule(sid, lookahead)
			if err != nil {
				return nil, err
			}

			emails = appendIfMissing(emails, e...)
		}

		return emails, nil
	}

	managedGroupNames := map[string]struct{}{}
	for _, schedule := range config.Schedules {
		logrus.Infof("checking slack group: %s", schedule.CurrentOnCallGroupName)

		currentOncallEngineerEmails, err := getEmailsForSchedules(schedule.ScheduleIDs, time.Second)
		if err != nil {
			logrus.Errorf("failed to get emails for %s: %v", schedule.CurrentOnCallGroupName, err)
			continue
		}

		err = updateSlackGroup(currentOncallEngineerEmails, schedule.CurrentOnCallGroupName)
		if err != nil {
			logrus.Errorf("failed to update slack group %s: %v", schedule.CurrentOnCallGroupName, err)
			continue
		}

		logrus.Infof("checking slack group: %s", schedule.AllOnCallGroupName)

		allOncallEngineerEmails, err := getEmailsForSchedules(schedule.ScheduleIDs, config.PagerdutyScheduleLookahead)
		if err != nil {
			logrus.Errorf("failed to get emails for %s: %v", schedule.AllOnCallGroupName, err)
			continue
		}

		err = updateSlackGroup(allOncallEngineerEmails, schedule.AllOnCallGroupName)
		if err != nil {
			logrus.Errorf("failed to update slack group %s: %v", schedule.AllOnCallGroupName, err)
			continue
		}

		managedGroupNames[schedule.CurrentOnCallGroupName] = struct{}{}
		managedGroupNames[schedule.AllOnCallGroupName] = struct{}{}
	}

	if len(managedGroupNames) == 0 {
		return nil
	}

	currentGroups := []*slack.UserGroup{}
	currentGroups = append(currentGroups, s.findUserGroupByPrefix(CurrentOncallGroupPrefix)...)
	currentGroups = append(currentGroups, s.findUserGroupByPrefix(AllOncallGroupPrefix)...)
	for _, g := range currentGroups {
		if _, ok := managedGroupNames[g.Name]; !ok {
			_, err := s.Client.DisableUserGroup(g.Name)
			if err != nil {
				logrus.Errorf("failed to disable unmanaged user group %s, %v", g.Name, err)
				continue
			}
		}
	}

	return nil
}

func appendIfMissing(slice []string, items ...string) []string {
out:
	for _, i := range items {
		for _, ele := range slice {
			if ele == i {
				continue out
			}
		}
		slice = append(slice, i)
	}

	return slice
}
