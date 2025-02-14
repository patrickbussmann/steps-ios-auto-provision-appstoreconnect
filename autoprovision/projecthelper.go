package autoprovision

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/sliceutil"
	project "github.com/bitrise-io/xcode-project"
	"github.com/bitrise-io/xcode-project/serialized"
	"github.com/bitrise-io/xcode-project/xcodeproj"
	"github.com/bitrise-io/xcode-project/xcscheme"
	"howett.net/plist"
)

// ProjectHelper ...
type ProjectHelper struct {
	MainTarget    xcodeproj.Target
	Targets       []xcodeproj.Target
	XcProj        xcodeproj.XcodeProj
	Configuration string

	buildSettingsCache map[string]map[string]serialized.Object // target/config/buildSettings(serialized.Object)
}

// NewProjectHelper checks the provided project or workspace and generate a ProjectHelper with the provided scheme and configuration
// Previously in the ruby version the initialize method did the same
// It returns a new ProjectHelper pointer and a configuration to use.
func NewProjectHelper(projOrWSPath, schemeName, configurationName string) (*ProjectHelper, string, error) {
	// Maybe we should do this checks during the input parsing
	if exits, err := pathutil.IsPathExists(projOrWSPath); err != nil {
		return nil, "", err
	} else if !exits {
		return nil, "", fmt.Errorf("provided path does not exists: %s", projOrWSPath)
	}

	// Get the project of the provided .xcodeproj or .xcworkspace
	xcproj, err := findBuiltProject(projOrWSPath, schemeName, configurationName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find build project: %s", err)
	}

	mainTarget, err := mainTargetOfScheme(xcproj, schemeName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find the main target of the scheme (%s): %s", schemeName, err)
	}

	scheme, _, err := xcproj.Scheme(schemeName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find scheme with name: %s in project: %s: %s", schemeName, projOrWSPath, err)
	}

	// Check if the archive is available for the scheme or not
	if _, archivable := scheme.AppBuildActionEntry(); !archivable {
		return nil, "", fmt.Errorf("archive action not defined for scheme: %s", scheme.Name)
	}

	// Configuration
	conf, err := configuration(configurationName, *scheme, xcproj)
	if err != nil {
		return nil, "", err
	}
	return &ProjectHelper{
			MainTarget:    mainTarget,
			Targets:       xcproj.Proj.Targets,
			XcProj:        xcproj,
			Configuration: conf,
		}, conf,
		nil
}

// ArchivableTargetBundleIDToEntitlements ...
func (p *ProjectHelper) ArchivableTargetBundleIDToEntitlements() (map[string]serialized.Object, error) {
	targets := append([]xcodeproj.Target{p.MainTarget}, p.MainTarget.DependentExecutableProductTargets(false)...)

	entitlementsByBundleID := map[string]serialized.Object{}

	for _, target := range targets {
		bundleID, err := p.TargetBundleID(target.Name, p.Configuration)
		if err != nil {
			return nil, fmt.Errorf("failed to get target (%s) bundle id: %s", target.Name, err)
		}

		entitlements, err := p.targetEntitlements(target.Name, p.Configuration, bundleID)
		if err != nil && !serialized.IsKeyNotFoundError(err) {
			return nil, fmt.Errorf("failed to get target (%s) bundle id: %s", target.Name, err)
		}

		entitlementsByBundleID[bundleID] = entitlements
	}

	return entitlementsByBundleID, nil
}

// Platform get the platform (PLATFORM_DISPLAY_NAME) - iOS, tvOS, macOS
func (p *ProjectHelper) Platform(configurationName string) (Platform, error) {
	settings, err := p.targetBuildSettings(p.MainTarget.Name, configurationName)
	if err != nil {
		return "", fmt.Errorf("failed to fetch project (%s) build settings: %s", p.XcProj.Path, err)
	}

	platformDisplayName, err := settings.String("PLATFORM_DISPLAY_NAME")
	if err != nil {
		return "", fmt.Errorf("no PLATFORM_DISPLAY_NAME config found for (%s) target", p.MainTarget.Name)
	}

	if platformDisplayName != string(IOS) && platformDisplayName != string(MacOS) && platformDisplayName != string(TVOS) {
		return "", fmt.Errorf("not supported platform. Platform (PLATFORM_DISPLAY_NAME) = %s, supported: %s, %s", platformDisplayName, IOS, TVOS)
	}
	return Platform(platformDisplayName), nil
}

// ProjectTeamID returns the development team's ID
// If there is mutlitple development team in the project (different team for targets) it will return an error
// It returns the development team's ID
func (p *ProjectHelper) ProjectTeamID(config string) (string, error) {
	var teamID string

	for _, target := range p.Targets {
		currentTeamID, err := p.targetTeamID(target.Name, config)
		if err != nil {
			log.Debugf("%", err)
		} else {
			log.Debugf("Target (%s) build settings/DEVELOPMENT_TEAM Team ID: %s", target.Name, currentTeamID)
		}

		if currentTeamID == "" {
			targetAttributes, err := p.XcProj.Proj.Attributes.TargetAttributes.Object(target.ID)
			if err != nil {
				return "", fmt.Errorf("failed to parse target (%s) attributes: %s", target.ID, err)
			}

			targetAttributesTeamID, err := targetAttributes.String("DevelopmentTeam")
			if err != nil && !serialized.IsKeyNotFoundError(err) {
				return "", fmt.Errorf("failed to parse development team for target (%s): %s", target.ID, err)
			}

			log.Debugf("Target (%s) DevelopmentTeam attribute: %s", target.Name, targetAttributesTeamID)

			if targetAttributesTeamID == "" {
				log.Debugf("Target (%s): No Team ID found.", target.Name)
				continue
			}

			currentTeamID = targetAttributesTeamID
		}

		if teamID == "" {
			teamID = currentTeamID
			continue
		}

		if teamID != currentTeamID {
			log.Warnf("Target (%s) Team ID (%s) does not match to the already registered team ID: %s\nThis causes build issue like: `Embedded binary is not signed with the same certificate as the parent app. Verify the embedded binary target's code sign settings match the parent app's.`", target.Name, currentTeamID, teamID)
			teamID = ""
			break
		}
	}

	return teamID, nil
}

func (p *ProjectHelper) targetTeamID(targatName, config string) (string, error) {
	settings, err := p.targetBuildSettings(targatName, config)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Team ID from target settings (%s): %s", targatName, err)
	}

	devTeam, err := settings.String("DEVELOPMENT_TEAM")
	if serialized.IsKeyNotFoundError(err) {
		return "", nil
	}
	return devTeam, err

}

func (p *ProjectHelper) targetBuildSettings(name, conf string) (serialized.Object, error) {
	targetCache, ok := p.buildSettingsCache[name]
	if ok {
		confCache, ok := targetCache[conf]
		if ok {
			return confCache, nil
		}
	}

	settings, err := p.XcProj.TargetBuildSettings(name, conf)
	if err != nil {
		return nil, err
	}

	if targetCache == nil {
		targetCache = map[string]serialized.Object{}
	}
	targetCache[conf] = settings

	if p.buildSettingsCache == nil {
		p.buildSettingsCache = map[string]map[string]serialized.Object{}
	}
	p.buildSettingsCache[name] = targetCache

	return settings, nil
}

// TargetBundleID returns the target bundle ID
// First it tries to fetch the bundle ID from the `PRODUCT_BUNDLE_IDENTIFIER` build settings
// If it's no available it will fetch the target's Info.plist and search for the `CFBundleIdentifier` key.
// The CFBundleIdentifier's value is not resolved in the Info.plist, so it will try to resolve it by the resolveBundleID()
// It returns  the target bundle ID
func (p *ProjectHelper) TargetBundleID(name, conf string) (string, error) {
	settings, err := p.targetBuildSettings(name, conf)
	if err != nil {
		return "", fmt.Errorf("failed to fetch target (%s) settings: %s", name, err)
	}

	bundleID, err := settings.String("PRODUCT_BUNDLE_IDENTIFIER")
	if err != nil && !serialized.IsKeyNotFoundError(err) {
		return "", fmt.Errorf("failed to parse target (%s) build settings attribute PRODUCT_BUNDLE_IDENTIFIER: %s", name, err)
	}
	if bundleID != "" {
		return bundleID, nil
	}

	log.Debugf("PRODUCT_BUNDLE_IDENTIFIER env not found in 'xcodebuild -showBuildSettings -project %s -target %s -configuration %s command's output, checking the Info.plist file's CFBundleIdentifier property...", p.XcProj.Path, name, conf)

	infoPlistPath, err := settings.String("INFOPLIST_FILE")
	if err != nil {
		return "", fmt.Errorf("failed to find Info.plist file: %s", err)
	}
	infoPlistPath = path.Join(path.Dir(p.XcProj.Path), infoPlistPath)

	if infoPlistPath == "" {
		return "", fmt.Errorf("failed to to determine bundle id: xcodebuild -showBuildSettings does not contains PRODUCT_BUNDLE_IDENTIFIER nor INFOPLIST_FILE' unless info_plist_path")
	}

	b, err := fileutil.ReadBytesFromFile(infoPlistPath)
	if err != nil {
		return "", fmt.Errorf("failed to read Info.plist: %s", err)
	}

	var options map[string]interface{}
	if _, err := plist.Unmarshal(b, &options); err != nil {
		return "", fmt.Errorf("failed to unmarshal Info.plist: %s ", err)
	}

	bundleID, ok := options["CFBundleIdentifier"].(string)
	if !ok || bundleID == "" {
		return "", fmt.Errorf("failed to parse CFBundleIdentifier from the Info.plist")
	}

	if !strings.Contains(bundleID, "$") {
		return bundleID, nil
	}

	log.Debugf("CFBundleIdentifier defined with variable: %s, trying to resolve it...", bundleID)

	resolved, err := expandTargetSetting(bundleID, settings)
	if err != nil {
		return "", fmt.Errorf("failed to resolve bundle ID: %s", err)
	}

	log.Debugf("resolved CFBundleIdentifier: %s", resolved)

	return resolved, nil
}

func (p *ProjectHelper) targetEntitlements(name, config, bundleID string) (serialized.Object, error) {
	entitlements, err := p.XcProj.TargetCodeSignEntitlements(name, config)
	if err != nil && !serialized.IsKeyNotFoundError(err) {
		return nil, err
	}

	return resolveEntitlementVariables(Entitlement(entitlements), bundleID)
}

// resolveEntitlementVariables expands variables in the project entitlements.
// Entitlement values can contain variables, for example: `iCloud.$(CFBundleIdentifier)`.
// Expanding iCloud Container values only, as they are compared to the profile values later.
// Expand CFBundleIdentifier variable only, other variables are not yet supported.
func resolveEntitlementVariables(entitlements Entitlement, bundleID string) (serialized.Object, error) {
	containers, err := entitlements.ICloudContainers()
	if err != nil {
		return nil, err
	}

	if len(containers) == 0 {
		return serialized.Object(entitlements), nil
	}

	var expandedContainers []interface{}
	for _, container := range containers {
		if strings.ContainsRune(container, '$') {
			expanded, err := expandTargetSetting(container, serialized.Object{"CFBundleIdentifier": bundleID})
			if err != nil {
				log.Warnf("Ignoring iCloud container ID (%s) as can not expand variable: %v", container, err)
				continue
			}

			expandedContainers = append(expandedContainers, expanded)
			continue
		}

		expandedContainers = append(expandedContainers, container)
	}

	entitlements[iCloudIdentifiersEntitlementKey] = expandedContainers

	return serialized.Object(entitlements), nil
}

// 'iPhone Developer' should match to 'iPhone Developer: Bitrise Bot (ABCD)'
func codesignIdentitesMatch(identity1, identity2 string) bool {
	if strings.Contains(strings.ToLower(identity1), strings.ToLower(identity2)) {
		return true
	}
	if strings.Contains(strings.ToLower(identity2), strings.ToLower(identity1)) {
		return true
	}
	return false
}

func expandTargetSetting(value string, buildSettings serialized.Object) (string, error) {
	regexpStr := `^(.*)[$][({](.+?)([:].+)?[})](.*)$`
	r, err := regexp.Compile(regexpStr)
	if err != nil {
		return "", err
	}

	captures := r.FindStringSubmatch(value)

	if len(captures) < 5 {
		return "", fmt.Errorf("failed to match regex '%s' to %s target build setting", regexpStr, value)
	}

	prefix := captures[1]
	envKey := captures[2]
	suffix := captures[4]

	envValue, err := buildSettings.String(envKey)
	if err != nil {
		return "", fmt.Errorf("failed to find environment variable value for key %s: %s", envKey, err)
	}

	return prefix + envValue + suffix, nil
}

func configuration(configurationName string, scheme xcscheme.Scheme, xcproj xcodeproj.XcodeProj) (string, error) {
	defaultConfiguration := scheme.ArchiveAction.BuildConfiguration
	var configuration string
	if configurationName == "" || configurationName == defaultConfiguration {
		configuration = defaultConfiguration
	} else if configurationName != defaultConfiguration {
		for _, target := range xcproj.Proj.Targets {
			var configNames []string
			for _, conf := range target.BuildConfigurationList.BuildConfigurations {
				configNames = append(configNames, conf.Name)
			}
			if !sliceutil.IsStringInSlice(configurationName, configNames) {
				return "", fmt.Errorf("build configuration (%s) not defined for target: (%s)", configurationName, target.Name)
			}
		}
		log.Warnf("Using user defined build configuration: %s instead of the scheme's default one: %s.\nMake sure you use the same configuration in further steps.", configurationName, defaultConfiguration)
		configuration = configurationName
	}

	return configuration, nil
}

// mainTargetOfScheme return the main target
func mainTargetOfScheme(proj xcodeproj.XcodeProj, scheme string) (xcodeproj.Target, error) {
	projTargets := proj.Proj.Targets

	log.Printf("Get all schemes of %s", proj.Path)
	var schemes []xcscheme.Scheme
	schemes, err := proj.Schemes()
	for _, scheme := range schemes {
		log.Printf("Got scheme '%s' with path '%s'", scheme.Name, scheme.Path)
	}


	sch, _, err := proj.Scheme(scheme)
	if err != nil {
		return xcodeproj.Target{}, fmt.Errorf("failed to find scheme (%s) in project: %s", scheme, err)
	}

	var blueIdent string
	for _, entry := range sch.BuildAction.BuildActionEntries {
		if entry.BuildableReference.IsAppReference() {
			blueIdent = entry.BuildableReference.BlueprintIdentifier
			break
		}
	}

	// Search for the main target
	for _, t := range projTargets {
		if t.ID == blueIdent {
			return t, nil

		}
	}
	return xcodeproj.Target{}, fmt.Errorf("failed to find the project's main target for scheme (%s)", scheme)
}

// findBuiltProject returns the Xcode project which will be built for the provided scheme
func findBuiltProject(pth, schemeName, configurationName string) (xcodeproj.XcodeProj, error) {
	scheme, schemeContainerDir, err := project.Scheme(pth, schemeName)
	if err != nil {
		return xcodeproj.XcodeProj{}, fmt.Errorf("could not get scheme with name %s from path %s", schemeName, pth)
	}

	if configurationName == "" {
		configurationName = scheme.ArchiveAction.BuildConfiguration
	}

	if configurationName == "" {
		return xcodeproj.XcodeProj{}, fmt.Errorf("no configuration provided nor default defined for the scheme's (%s) archive action", schemeName)
	}

	archiveEntry, ok := scheme.AppBuildActionEntry()
	if !ok {
		return xcodeproj.XcodeProj{}, fmt.Errorf("archivable entry not found")
	}

	projectPth, err := archiveEntry.BuildableReference.ReferencedContainerAbsPath(filepath.Dir(schemeContainerDir))
	if err != nil {
		return xcodeproj.XcodeProj{}, err
	}

	xcodeProj, err := xcodeproj.Open(projectPth)
	if err != nil {
		return xcodeproj.XcodeProj{}, err
	}

	return xcodeProj, nil
}
