package autoprovision

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/xcode-project/serialized"
	"github.com/bitrise-io/xcode-project/xcodeproj"
)

var schemeCases []string
var targetCases []string
var xcProjCases []xcodeproj.XcodeProj
var projectCases []string
var projHelpCases []ProjectHelper
var configCases []string

func TestNew(t *testing.T) {
	var err error
	schemeCases, _, xcProjCases, projHelpCases, configCases, err = initTestCases()
	if err != nil {
		t.Fatalf("Failed to initialize test cases: %s", err)
	}

	tests := []struct {
		name              string
		projOrWSPath      string
		schemeName        string
		configurationName string
		wantConfiguration string
		wantErr           bool
	}{
		{
			name:              "Xcode 10 workspace - iOS",
			projOrWSPath:      xcProjCases[0].Path,
			schemeName:        "Xcode-10_default",
			configurationName: "Debug",
			wantConfiguration: "Debug",
			wantErr:           false,
		},
		{
			name:              "Xcode 10 workspace - iOS - Default configuration",
			projOrWSPath:      xcProjCases[0].Path,
			schemeName:        "Xcode-10_default",
			configurationName: "",
			wantConfiguration: "Release",
			wantErr:           false,
		},
		{
			name:              "Xcode 10 workspace - iOS - Default configuration - Gdańsk scheme",
			projOrWSPath:      xcProjCases[0].Path,
			schemeName:        "Gdańsk",
			configurationName: "",
			wantConfiguration: "Release",
			wantErr:           false,
		},
		{
			name:              "Xcode-10_mac project - MacOS - Debug configuration",
			projOrWSPath:      xcProjCases[2].Path,
			schemeName:        "Xcode-10_mac",
			configurationName: "Debug",
			wantConfiguration: "Debug",
			wantErr:           false,
		},
		{
			name:              "Xcode-10_mac project - MacOS - Default configuration",
			projOrWSPath:      xcProjCases[2].Path,
			schemeName:        "Xcode-10_mac",
			configurationName: "",
			wantConfiguration: "Release",
			wantErr:           false,
		},
		{
			name:              "TV_OS.xcodeproj project - TVOS - Default configuration",
			projOrWSPath:      xcProjCases[4].Path,
			schemeName:        "TV_OS",
			configurationName: "",
			wantConfiguration: "Release",
			wantErr:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projHelp, conf, err := NewProjectHelper(tt.projOrWSPath, tt.schemeName, tt.configurationName)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if projHelp == nil {
				t.Errorf("New() error = No projectHelper was generated")
			}
			if conf != tt.wantConfiguration {
				t.Errorf("New() got1 = %v, want %v", conf, tt.wantConfiguration)
			}
		})
	}
}

func TestProjectHelper_ProjectTeamID(t *testing.T) {
	log.SetEnableDebugLog(true)

	var err error
	schemeCases, _, _, projHelpCases, configCases, err = initTestCases()
	if err != nil {
		t.Fatalf("Failed to initialize test cases: %s", err)
	}

	tests := []struct {
		name    string
		config  string
		want    string
		wantErr bool
	}{
		{
			name:    schemeCases[0] + " Debug",
			config:  configCases[0],
			want:    "72SA8V3WYL",
			wantErr: false,
		},
		{
			name:    schemeCases[1] + " Release",
			config:  configCases[1],
			want:    "72SA8V3WYL",
			wantErr: false,
		},
		{
			name:    schemeCases[2] + " Debug",
			config:  configCases[2],
			want:    "72SA8V3WYL",
			wantErr: false,
		},
		{
			name:    schemeCases[3] + " Release",
			config:  configCases[3],
			want:    "72SA8V3WYL",
			wantErr: false,
		},
		{
			name:    schemeCases[4] + " Debug",
			config:  configCases[4],
			want:    "72SA8V3WYL",
			wantErr: false,
		},
		{
			name:    schemeCases[5] + " Release",
			config:  configCases[5],
			want:    "72SA8V3WYL",
			wantErr: false,
		},
	}

	for i, tt := range tests {
		p := projHelpCases[i]

		t.Run(tt.name, func(t *testing.T) {
			got, err := p.ProjectTeamID(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProjectHelper.ProjectTeamID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ProjectHelper.ProjectTeamID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_codesignIdentitesMatch(t *testing.T) {
	tests := []struct {
		name      string
		identity1 string
		identity2 string
		want      bool
	}{
		{
			name:      "Equal identities",
			identity1: "iPhone Developer",
			identity2: "iPhone Developer",
			want:      true,
		},
		{
			name:      "First identity contains the second one",
			identity1: "iPhone Developer: Bitrise Bot (ABCD)",
			identity2: "iPhone Developer",
			want:      true,
		},
		{
			name:      "Second identity contains the first one",
			identity1: "iPhone Developer",
			identity2: "iPhone Developer: Bitrise Bot (ABCD)",
			want:      true,
		},
		{
			name:      "Second identity empty",
			identity1: "iPhone Developer",
			identity2: "",
			want:      true,
		},
		{
			name:      "Identities not match",
			identity1: "iPhone Developer",
			identity2: "iPad Developer",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codesignIdentitesMatch(tt.identity1, tt.identity2); got != tt.want {
				t.Errorf("codesignIdentitesMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_expandTargetSetting(t *testing.T) {
	const productName = "Sample"
	tests := []struct {
		name          string
		value         string
		buildSettings map[string]interface{}
		want          string
		wantErr       bool
	}{
		{
			name:  "Bitrise.$(PRODUCT_NAME:rfc1034identifier)",
			value: "Bitrise.$(PRODUCT_NAME:rfc1034identifier)",
			buildSettings: func() map[string]interface{} {
				m := make(map[string]interface{})
				m["PRODUCT_NAME"] = productName
				return m
			}(),
			want:    fmt.Sprintf("Bitrise.%s", productName),
			wantErr: false,
		},
		{
			name:  "Bitrise.$(PRODUCT_NAME:rfc1034identifier)",
			value: "Bitrise.$(PRODUCT_NAME:rfc1034identifier)",
			buildSettings: func() map[string]interface{} {
				m := make(map[string]interface{})
				m["PRODUCT_NAME"] = productName
				m["a"] = productName
				return m
			}(),
			want:    fmt.Sprintf("Bitrise.%s", productName),
			wantErr: false,
		},
		{
			name:  "Bitrise.Test.$(PRODUCT_NAME:rfc1034identifier).Suffix",
			value: "Bitrise.Test.$(PRODUCT_NAME:rfc1034identifier).Suffix",
			buildSettings: func() map[string]interface{} {
				m := make(map[string]interface{})
				m["PRODUCT_NAME"] = productName
				m["a"] = productName
				return m
			}(),
			want:    fmt.Sprintf("Bitrise.Test.%s.Suffix", productName),
			wantErr: false,
		},
		{
			name:  "iCloud.$(CFBundleIdentifier)",
			value: "iCloud.$(CFBundleIdentifier)",
			buildSettings: func() map[string]interface{} {
				m := make(map[string]interface{})
				m["CFBundleIdentifier"] = productName
				return m
			}(),
			want:    fmt.Sprintf("iCloud.%s", productName),
			wantErr: false,
		},
		{
			name:  "iCloud.${CFBundleIdentifier}",
			value: "iCloud.${CFBundleIdentifier}",
			buildSettings: func() map[string]interface{} {
				m := make(map[string]interface{})
				m["CFBundleIdentifier"] = productName
				return m
			}(),
			want:    fmt.Sprintf("iCloud.%s", productName),
			wantErr: false,
		},
		{
			name:  "${CFBundleIdentifier}.Suffix",
			value: "${CFBundleIdentifier}.Suffix",
			buildSettings: func() map[string]interface{} {
				m := make(map[string]interface{})
				m["CFBundleIdentifier"] = productName
				return m
			}(),
			want:    fmt.Sprintf("%s.Suffix", productName),
			wantErr: false,
		},
		{
			name:  "$(CFBundleIdentifier)",
			value: "$(CFBundleIdentifier)",
			buildSettings: func() map[string]interface{} {
				m := make(map[string]interface{})
				m["CFBundleIdentifier"] = productName
				return m
			}(),
			want:    productName,
			wantErr: false,
		},
		{
			name:          "iCloud.bundle.id",
			value:         "iCloud.bundle.id",
			buildSettings: nil,
			want:          "",
			wantErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandTargetSetting(tt.value, tt.buildSettings)
			if (err != nil) != tt.wantErr {
				t.Errorf("expandTargetSetting() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("expandTargetSetting() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectHelper_TargetBundleID(t *testing.T) {
	var err error
	schemeCases, targetCases, xcProjCases, projHelpCases, configCases, err = initTestCases()
	if err != nil {
		t.Fatalf("Failed to initialize test cases: %s", err)
	}

	for i, schemeCase := range schemeCases {
		xcProj, err := findBuiltProject(
			projectCases[i],
			schemeCase,
			configCases[i],
		)
		if err != nil {
			t.Fatalf("Failed to generate XcodeProj for test case: %s", err)
		}
		xcProjCases = append(xcProjCases, xcProj)

		projHelp, _, err := NewProjectHelper(
			projectCases[i],
			schemeCase,
			configCases[i],
		)
		if err != nil {
			t.Fatalf("Failed to generate projectHelper for test case: %s", err)
		}
		projHelpCases = append(projHelpCases, *projHelp)
	}

	tests := []struct {
		name       string
		targetName string
		conf       string
		want       string
		wantErr    bool
	}{
		{
			name:       targetCases[0] + " Debug",
			targetName: targetCases[0],
			conf:       configCases[0],
			want:       "com.bitrise.Xcode-10-default",
			wantErr:    false,
		},
		{
			name:       targetCases[1] + " Release",
			targetName: targetCases[1],
			conf:       configCases[1],
			want:       "com.bitrise.Xcode-10-default",
			wantErr:    false,
		},
		{
			name:       targetCases[2] + " Release",
			targetName: targetCases[2],
			conf:       configCases[2],
			want:       "com.bitrise.Xcode-10-mac",
			wantErr:    false,
		},
		{
			name:       targetCases[3] + " Release",
			targetName: targetCases[3],
			conf:       configCases[3],
			want:       "com.bitrise.Xcode-10-mac",
			wantErr:    false,
		},
		{
			name:       targetCases[4] + " Release",
			targetName: targetCases[4],
			conf:       configCases[4],
			want:       "com.bitrise.TV-OS",
			wantErr:    false,
		},
		{
			name:       targetCases[5] + " Release",
			targetName: targetCases[5],
			conf:       configCases[5],
			want:       "com.bitrise.TV-OS",
			wantErr:    false,
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := projHelpCases[i]

			got, err := p.TargetBundleID(tt.targetName, tt.conf)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProjectHelper.TargetBundleID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ProjectHelper.TargetBundleID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func initTestCases() ([]string, []string, []xcodeproj.XcodeProj, []ProjectHelper, []string, error) {
	//
	// If the test cases already initialized return them
	if schemeCases != nil {
		return schemeCases, targetCases, xcProjCases, projHelpCases, configCases, nil
	}

	p, err := pathutil.NormalizedOSTempDirPath("_autoprov")
	if err != nil {
		log.Errorf("Failed to create tmp dir error: %s", err)
	}
	cmd := command.New("git", "clone", "-b", "project", "https://github.com/bitrise-io/sample-artifacts.git", p).SetStderr(os.Stderr).SetStdout(os.Stdout)
	if err := cmd.Run(); err != nil {
		log.Errorf("Failed to git clone the sample project files error: %s", err)
	}
	//
	// Init test cases
	targetCases = []string{
		"Xcode-10_default",
		"Xcode-10_default",
		"Xcode-10_mac",
		"Xcode-10_mac",
		"TV_OS",
		"TV_OS",
	}

	schemeCases = []string{
		"Xcode-10_default",
		"Xcode-10_default",
		"Xcode-10_mac",
		"Xcode-10_mac",
		"TV_OS",
		"TV_OS",
	}
	configCases = []string{
		"Debug",
		"Release",
		"Debug",
		"Release",
		"Debug",
		"Release",
	}
	projectCases = []string{
		p + "/ios_project_files/Xcode-10_default.xcworkspace",
		p + "/ios_project_files/Xcode-10_default.xcworkspace",
		p + "/ios_project_files/Xcode-10_mac.xcodeproj",
		p + "/ios_project_files/Xcode-10_mac.xcodeproj",
		p + "/ios_project_files/TV_OS.xcodeproj",
		p + "/ios_project_files/TV_OS.xcodeproj",
	}
	var xcProjCases []xcodeproj.XcodeProj
	var projHelpCases []ProjectHelper

	for i, schemeCase := range schemeCases {
		xcProj, err := findBuiltProject(
			projectCases[i],
			schemeCase,
			configCases[i],
		)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("Failed to generate XcodeProj for test case: %s", err)
		}
		xcProjCases = append(xcProjCases, xcProj)

		projHelp, _, err := NewProjectHelper(
			projectCases[i],
			schemeCase,
			configCases[i],
		)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("Failed to generate projectHelper for test case: %s", err)
		}
		projHelpCases = append(projHelpCases, *projHelp)
	}

	return schemeCases, targetCases, xcProjCases, projHelpCases, configCases, nil
}

func TestProjectHelper_targetEntitlements(t *testing.T) {
	var err error
	schemeCases, targetCases, xcProjCases, projHelpCases, configCases, err = initTestCases()
	if err != nil {
		t.Fatalf("Failed to initialize test cases: %s", err)
	}

	tests := []struct {
		name          string
		targetName    string
		conf          string
		bundleID      string
		want          serialized.Object
		projectHelper ProjectHelper
		wantErr       bool
	}{
		{
			name:          targetCases[2] + " Release",
			targetName:    targetCases[2],
			conf:          configCases[2],
			projectHelper: projHelpCases[2],
			want: func() serialized.Object {
				m := make(map[string]interface{})
				m["com.apple.security.app-sandbox"] = true
				m["com.apple.security.files.user-selected.read-only"] = true
				return m
			}(),
			wantErr: false,
		},
		{
			name:          targetCases[3] + " Release",
			targetName:    targetCases[3],
			conf:          configCases[3],
			projectHelper: projHelpCases[3],
			want: func() serialized.Object {
				m := make(map[string]interface{})
				m["com.apple.security.app-sandbox"] = true
				m["com.apple.security.files.user-selected.read-only"] = true
				return m
			}(),
			wantErr: false,
		},
		{
			name:          targetCases[4] + " Release",
			targetName:    targetCases[4],
			conf:          configCases[4],
			projectHelper: projHelpCases[4],
			want:          nil,
			wantErr:       false,
		},
		{
			name:          targetCases[5] + " Release",
			targetName:    targetCases[5],
			conf:          configCases[5],
			projectHelper: projHelpCases[5],
			want:          nil,
			wantErr:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.projectHelper.targetEntitlements(tt.targetName, tt.conf, tt.bundleID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProjectHelper.targetEntitlements() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ProjectHelper.targetEntitlements() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_resolveEntitlementVariables(t *testing.T) {
	type args struct {
		entitlements Entitlement
		bundleID     string
	}
	tests := []struct {
		name    string
		args    args
		want    serialized.Object
		wantErr bool
	}{
		{
			name: "Existing entitlememts are unchanged",
			args: args{
				entitlements: map[string]interface{}{
					"com.apple.developer.contacts.notes": true,
				},
			},
			want: map[string]interface{}{
				"com.apple.developer.contacts.notes": true,
			},
		},
		{
			name: "iCloud entitlememts are unchanged, if service is in use",
			args: args{
				entitlements: map[string]interface{}{
					"com.apple.developer.icloud-services": []interface{}{"CloudKit"},
					"com.apple.developer.icloud-container-identifiers": []interface{}{
						"iCloud.bundle.id",
					},
				},
			},
			want: map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{"CloudKit"},
				"com.apple.developer.icloud-container-identifiers": []interface{}{
					"iCloud.bundle.id",
				},
			},
		},
		{
			name: "iCloud entitlememts are unchanged, if service is not in use",
			args: args{
				entitlements: map[string]interface{}{
					"com.apple.developer.icloud-services": []interface{}{},
					"com.apple.developer.icloud-container-identifiers": []interface{}{
						"iCloud.bundle.id",
					},
				},
			},
			want: map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{},
				"com.apple.developer.icloud-container-identifiers": []interface{}{
					"iCloud.bundle.id",
				},
			},
		},
		{
			name: "iCloud containers CFBundleIdentifier variable is expanded",
			args: args{
				entitlements: map[string]interface{}{
					"com.apple.developer.icloud-services": []interface{}{"CloudKit"},
					"com.apple.developer.icloud-container-identifiers": []interface{}{
						"iCloud.${CFBundleIdentifier}",
					},
				},
				bundleID: "bundle.id",
			},
			want: map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{"CloudKit"},
				"com.apple.developer.icloud-container-identifiers": []interface{}{
					"iCloud.bundle.id",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveEntitlementVariables(tt.args.entitlements, tt.args.bundleID)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveEntitlementVariables() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveEntitlementVariables() = %v, want %v", got, tt.want)
			}
		})
	}
}
