package pf

import "strings"

func splitAnchor(anchorName string) (parent string, child string, nested bool) {
	anchorName = strings.Trim(anchorName, "/")
	if anchorName == "" {
		return "", "", false
	}
	idx := strings.LastIndex(anchorName, "/")
	if idx < 0 {
		return "", anchorName, false
	}
	return anchorName[:idx], anchorName[idx+1:], true
}

func anchorOutputContains(output string, anchorName string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == anchorName {
			return true
		}
	}
	return false
}

func parseEnabled(info string) bool {
	for _, line := range strings.Split(info, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "Status:" {
			return fields[1] == "Enabled"
		}
	}
	return false
}
