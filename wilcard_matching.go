package main

func isMatch(s string, p string) bool {
	sidx, pidx := 0, 0
	staridx := -1
	matchidx := 0

	for sidx < len(s) {
		if pidx < len(p) && (p[pidx] == '?' || s[sidx] == p[pidx]) {
			sidx++
			pidx++
		} else if pidx < len(p) && p[pidx] == '*' {
			staridx = pidx
			matchidx = sidx
			pidx++
		} else if staridx != -1 {

		}

	}

}
