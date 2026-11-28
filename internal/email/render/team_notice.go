package render

import "strings"

// TeamNotice is an email to the merchant's own team from a collection playbook: an internal alert or a task.
type TeamNotice struct {
	Merchant     string
	ContactEmail string
	// Heading says which playbook and invoice the notice is about.
	Heading string
	// Body is the step's note with its variables filled in; blank lines separate paragraphs.
	Body string
	// ViewURL opens the hosted invoice; empty means no link.
	ViewURL string
}

// Paragraphs splits Body at blank lines into paragraphs of lines.
func (v TeamNotice) Paragraphs() [][]string {
	return Reminder{Body: v.Body}.Paragraphs()
}

var teamNoticePage = newPage("collections_team_notice")

// CollectionsTeamNotice renders an internal alert or task email.
func CollectionsTeamNotice(subject string, v TeamNotice) (Content, error) {
	v.Heading = strings.TrimSpace(v.Heading)
	return teamNoticePage.render(subject, v)
}
