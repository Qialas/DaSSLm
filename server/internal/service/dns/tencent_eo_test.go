package dns

import "testing"

func TestTencentEORecordName(t *testing.T) {
	cases := []struct {
		rr       string
		zoneName string
		want     string
	}{
		{rr: "@", zoneName: "example.com", want: "example.com"},
		{rr: "www", zoneName: "example.com", want: "www.example.com"},
		{rr: "api.example.com", zoneName: "example.com", want: "api.example.com"},
	}
	for _, tc := range cases {
		if got := tencentEORecordName(tc.rr, tc.zoneName); got != tc.want {
			t.Fatalf("record name for %q/%q: want %q, got %q", tc.rr, tc.zoneName, tc.want, got)
		}
	}
}

func TestTencentEORR(t *testing.T) {
	cases := []struct {
		name     string
		zoneName string
		want     string
	}{
		{name: "example.com", zoneName: "example.com", want: "@"},
		{name: "www.example.com", zoneName: "example.com", want: "www"},
		{name: "external.example.net", zoneName: "example.com", want: "external.example.net"},
	}
	for _, tc := range cases {
		if got := tencentEORR(tc.name, tc.zoneName); got != tc.want {
			t.Fatalf("rr for %q/%q: want %q, got %q", tc.name, tc.zoneName, tc.want, got)
		}
	}
}

func TestSplitTencentEORef(t *testing.T) {
	zoneID, zoneName := splitTencentEORef("zone-abc|example.com")
	if zoneID != "zone-abc" || zoneName != "example.com" {
		t.Fatalf("unexpected split: %q %q", zoneID, zoneName)
	}
	zoneID, zoneName = splitTencentEORef("example.com")
	if zoneID != "" || zoneName != "example.com" {
		t.Fatalf("unexpected domain split: %q %q", zoneID, zoneName)
	}
}

func TestTencentEORecordPayloadOmitsLocation(t *testing.T) {
	payload := tencentEORecordPayload("zone-abc", "example.com", RecordInput{
		RR:    "www",
		Type:  "A",
		Value: "192.0.2.1",
		Line:  "CustomLine",
		TTL:   600,
	})
	if _, ok := payload["Location"]; ok {
		t.Fatalf("expected EO record payload to omit Location, got %#v", payload)
	}
}
