package ruleset

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestListSource(t *testing.T) {
	src, err := ListSource(`# a comment
example.com
+.example.org
DOMAIN-SUFFIX,suffix.net
DOMAIN,exact.io # trailing
DOMAIN-KEYWORD,openai
IP-CIDR,1.2.3.0/24,no-resolve
IP-CIDR6,2001:db8::/32
5.6.7.8
not a domain
`)
	if err != nil {
		t.Fatal(err)
	}
	var rs struct {
		Version int `json:"version"`
		Rules   []map[string][]string
	}
	if err := json.Unmarshal(src, &rs); err != nil || len(rs.Rules) != 1 {
		t.Fatalf("%s: %v", src, err)
	}
	want := map[string][]string{
		"domain_suffix":  {"example.com", "example.org", "suffix.net"},
		"domain":         {"exact.io"},
		"domain_keyword": {"openai"},
		"ip_cidr":        {"1.2.3.0/24", "2001:db8::/32", "5.6.7.8"},
	}
	if !reflect.DeepEqual(rs.Rules[0], want) {
		t.Fatalf("got %v", rs.Rules[0])
	}
	if _, err := ListSource("<html>nothing here</html>"); err == nil {
		t.Fatal("a page with no entries must be an error")
	}
}

func TestURLTagStable(t *testing.T) {
	a, b := URLTag("https://x.test/list.txt"), URLTag(" https://x.test/list.txt ")
	if a != b || len(a) != 14 || URLTag("https://x.test/other.txt") == a {
		t.Fatalf("tags: %s %s", a, b)
	}
}
