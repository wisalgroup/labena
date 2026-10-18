package enums

import (
	"encoding/json"
	"slices"
	"testing"
)

func checkValues[T interface {
	~string
	Valid() bool
}](t *testing.T, values func() []T, want []T) {
	t.Helper()
	if !slices.Equal(values(), want) {
		t.Fatalf("values = %v, want %v", values(), want)
	}
	for _, v := range values() {
		if !v.Valid() {
			t.Fatalf("declared value %q is invalid", v)
		}
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var decoded T
		if err = json.Unmarshal(data, &decoded); err != nil || decoded != v {
			t.Fatalf("JSON round trip: %q %v", decoded, err)
		}
	}
	first := values()
	first[0] = T("changed")
	if !slices.Equal(values(), want) {
		t.Fatal("caller mutated shared values")
	}
	for _, raw := range []string{"", "unknown"} {
		var decoded T
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Valid() {
			t.Fatalf("unsupported value %q accepted", decoded)
		}
	}
	var decoded T
	if err := json.Unmarshal([]byte(`123`), &decoded); err == nil {
		t.Fatal("number accepted as string enum")
	}
}

func TestIdentityEnums(t *testing.T) {
	checkValues(t, UserStatusValues, []UserStatus{UserActive, UserSuspended, UserPendingDeletion, UserDeleted})
	checkValues(t, AuthProviderValues, []AuthProvider{ProviderGoogle, ProviderApple})
	checkValues(t, VerificationPurposeValues, []VerificationPurpose{PurposeVerifyEmail, PurposeRegistration, PurposeSignIn, PurposeLinkContact, PurposeChangeContact, PurposeRecovery})
	checkValues(t, VerificationChannelValues, []VerificationChannel{ChannelEmail, ChannelSMS})
	checkValues(t, SessionPlatformValues, []SessionPlatform{PlatformIOS, PlatformAndroid, PlatformWeb})
}
