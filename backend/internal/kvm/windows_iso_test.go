package kvm

import (
	"encoding/xml"
	"strings"
	"testing"
)

// Trimmed from `wimlib-imagex info` on the official zh-CN Server 2025 ISO:
// index 1/3 are Server Core, 2/4 are the desktop editions.
const server2025WimInfo = `WIM Information:
----------------
GUID:        0x1234
Image Count: 4
Compression: LZX

Image Details:
--------------
Index:                  1
Name:                   Windows Server 2025 SERVERSTANDARDCORE
Display Name:           Windows Server 2025 Standard
Architecture:           x86_64
Edition ID:             ServerStandard
Installation Type:      Server Core
Product Type:           ServerNT
Languages:              zh-CN
Default Language:       zh-CN
Major Version:          10
Minor Version:          0
Build:                  26100
Service Pack Build:     6584

Index:                  2
Name:                   Windows Server 2025 SERVERSTANDARD
Display Name:           Windows Server 2025 Standard (桌面体验)
Architecture:           x86_64
Edition ID:             ServerStandard
Installation Type:      Server
Product Type:           ServerNT
Languages:              zh-CN
Default Language:       zh-CN
Major Version:          10
Minor Version:          0
Build:                  26100
Service Pack Build:     6584

Index:                  3
Name:                   Windows Server 2025 SERVERDATACENTERCORE
Architecture:           x86_64
Edition ID:             ServerDatacenter
Installation Type:      Server Core
Product Type:           ServerNT
Default Language:       zh-CN
Build:                  26100
Service Pack Build:     6584

Index:                  4
Name:                   Windows Server 2025 SERVERDATACENTER
Architecture:           x86_64
Edition ID:             ServerDatacenter
Installation Type:      Server
Product Type:           ServerNT
Default Language:       zh-CN
Build:                  26100
Service Pack Build:     6584
`

func TestParseWimlibInfoPicksDesktopEdition(t *testing.T) {
	images := parseWimlibInfo(server2025WimInfo)
	if len(images) != 4 {
		t.Fatalf("parsed %d images, want 4", len(images))
	}
	// "Service Pack Build" must not be mistaken for "Build".
	if images[0].Build != 26100 || images[0].Index != 1 {
		t.Fatalf("first image = %+v", images[0])
	}
	if images[1].DisplayName == "" {
		t.Fatalf("display name was not parsed: %+v", images[1])
	}

	chosen := chooseWindowsImage(images)
	if chosen.Index != 2 {
		t.Fatalf("chose index %d (%s), want 2 (SERVERSTANDARD desktop)", chosen.Index, chosen.EditionID)
	}
	if got := windowsProductKey(chosen); got != "TVRH6-WHNXV-R9WG3-9XRFY-MY832" {
		t.Fatalf("Server 2025 Standard product key = %q", got)
	}
	if chosen.Build < 22000 {
		t.Fatalf("build %d should require the TPM/SecureBoot bypass", chosen.Build)
	}
}

func TestWindowsProductKeyFollowsEditionAndBuild(t *testing.T) {
	cases := []struct {
		edition string
		build   int
		want    string
	}{
		{"ServerStandard", 17763, "N69G4-B89J2-4G8F4-WWYCC-J464C"},
		{"ServerStandard", 20348, "VDYBN-27WPP-V4HQT-9VMD4-VMK7H"},
		{"ServerStandard", 26100, "TVRH6-WHNXV-R9WG3-9XRFY-MY832"},
		{"ServerDatacenter", 26100, "D764K-2NDRG-47T6Q-P8T8W-YP6DF"},
		{"Professional", 22631, "VK7JG-NPHTM-C97JM-9MPGT-3V66T"},
		{"EnterpriseS", 19045, ""},
	}
	for _, testCase := range cases {
		image := windowsWimImage{EditionID: testCase.edition, ProductType: "ServerNT", Build: testCase.build}
		if testCase.edition == "Professional" {
			image.ProductType = "WinNT"
		}
		if got := windowsProductKey(image); got != testCase.want {
			t.Fatalf("%s build %d: key = %q, want %q", testCase.edition, testCase.build, got, testCase.want)
		}
	}
}

func TestWindowsUnattendAdaptsToProfile(t *testing.T) {
	english := windowsAutounattendXML("plain", "Password123!", windowsInstallProfile{Index: 1, Language: "en-US"})
	if !strings.Contains(english, "<UILanguage>en-US</UILanguage>") {
		t.Fatal("en-US profile should keep the en-US locale")
	}
	if strings.Contains(english, "<ProductKey>") {
		t.Fatal("a profile without a product key must not emit one")
	}
	if !strings.Contains(english, "<Value>1</Value>") {
		t.Fatal("index 1 should stay untouched")
	}

	chinese := windowsAutounattendXML("cn", "Password123!", windowsInstallProfile{
		Index:      2,
		Language:   "zh-CN",
		ProductKey: "TVRH6-WHNXV-R9WG3-9XRFY-MY832",
	})
	if strings.Contains(chinese, "en-US") {
		t.Fatal("zh-CN profile still contains en-US locale settings")
	}
	if !strings.Contains(chinese, "<UILanguage>zh-CN</UILanguage>") {
		t.Fatal("zh-CN language was not applied")
	}
	if !strings.Contains(chinese, "<Value>2</Value>") {
		t.Fatal("/IMAGE/INDEX was not switched to 2")
	}
	if !strings.Contains(chinese, "<ProductKey><Key>TVRH6-WHNXV-R9WG3-9XRFY-MY832</Key>") {
		t.Fatal("product key was not injected")
	}
	var document struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal([]byte(chinese), &document); err != nil {
		t.Fatalf("adapted answer file is not valid XML: %v", err)
	}
}

func TestLanguageFromFileNameFallback(t *testing.T) {
	cases := map[string]string{
		"/x/zh-cn_windows_server_2025_x64_dvd.iso": "zh-CN",
		"/x/en-us_windows_server_2022_x64.iso":     "en-US",
		"/x/cn_windows_server_2019_x64_dvd.iso":    "zh-CN",
		"/x/whatever.iso":                          "en-US",
	}
	for path, want := range cases {
		if got := languageFromFileName(path); got != want {
			t.Fatalf("%s: language = %q, want %q", path, got, want)
		}
	}
}
