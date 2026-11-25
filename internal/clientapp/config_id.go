package clientapp

import "regexp"

var configID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validConfigID(value string) bool {
	return configID.MatchString(value) && value != "00000000-0000-0000-0000-000000000000"
}
