package auth

import (
	"reflect"
	"testing"
	"time"
)

func TestIssueTopicClientIndependentExactGrants(t *testing.T) {
	m := New("test-topic-client-signing-secret")
	for _, tc := range []struct {
		name               string
		subscribe, publish []string
	}{
		{"both", []string{"/iot/external/74/read"}, []string{"/iot/external/74/write"}},
		{"publish only", nil, []string{"/iot/external/74/write"}},
		{"subscribe only", []string{"/iot/external/74/read"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := m.IssueTopicClient("iot-topic-client", "t", tc.subscribe, tc.publish, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			c, err := m.Parse(token)
			if err != nil || c.TokenUse != "topic-consumer" || len(c.Scopes) != 0 {
				t.Fatal("broker token lost isolation", err)
			}
			want := []ACLRule{}
			for _, topic := range tc.subscribe {
				want = append(want, ACLRule{Permission: "allow", Action: "subscribe", Topic: topic})
			}
			for _, topic := range tc.publish {
				want = append(want, ACLRule{Permission: "allow", Action: "publish", Topic: topic})
			}
			want = append(want, ACLRule{Permission: "deny", Action: "all", Topic: "#"}, ACLRule{Permission: "deny", Action: "all", Topic: "$SYS/#"})
			if !reflect.DeepEqual(c.ACL, want) {
				t.Fatal("unexpected ACL grants", c.ACL)
			}
		})
	}
	for _, topic := range []string{"", "/x/+", "/x/#", "/x/\x00"} {
		for _, publish := range []bool{false, true} {
			sub, pub := []string{topic}, []string(nil)
			if publish {
				sub, pub = nil, sub
			}
			if _, err := m.IssueTopicClient("iot-topic-client", "t", sub, pub, time.Now().Add(time.Hour)); err == nil {
				t.Fatal("invalid exact grant accepted")
			}
		}
	}
	if _, err := m.IssueTopicClient("iot-topic-client", "t", nil, nil, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("empty grants accepted")
	}
}
