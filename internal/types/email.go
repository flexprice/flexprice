package types

import "regexp"

func IsValidEmail(email string) bool {
	if email == "" || !emailRegex.MatchString(email) {
		return false
	}
	return true
}

// Email Validation regex
// ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$
// ^: Start of the string
// [a-zA-Z0-9._%+-]+: One or more characters that match the regex [a-zA-Z0-9._%+-]
// @: The @ symbol
// [a-zA-Z0-9.-]+: One or more characters that match the regex [a-zA-Z0-9.-]
// \.: The . symbol
// [a-zA-Z]{2,}: One or more characters that match the regex [a-zA-Z]
// $: End of the string
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
