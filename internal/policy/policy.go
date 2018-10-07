package policy

import (
	"strings"

	"github.com/keel-hq/keel/types"

	log "github.com/sirupsen/logrus"
)

type PolicyType int

const (
	PolicyTypeNone PolicyType = iota
	PolicyTypeSemver
	PolicyTypeForce
	PolicyTypeGlob
	PolicyTypeRegexp
)

type Policy interface {
	ShouldUpdate(current, new string) (bool, error)
	Name() string
	Type() PolicyType
}

type NilPolicy struct{}

func (np *NilPolicy) ShouldUpdate(c, n string) (bool, error) { return false, nil }
func (np *NilPolicy) Name() string                           { return "nil policy" }
func (np *NilPolicy) Type() PolicyType                       { return PolicyTypeNone }

// GetPolicyFromLabels - gets policy from k8s labels
func GetPolicyFromLabels(labels map[string]string) Policy {

	policyName, ok := getPolicyFromLabels(labels)
	if !ok {
		return &NilPolicy{}
	}

	return GetPolicy(policyName, &Options{MatchTag: getMatchTag(labels)})

	// switch policyName {
	// case "all", "major", "minor", "patch":
	// 	return ParseSemverPolicy(policyName)
	// case "force":
	// 	matchTag := getMatchTag(labels)
	// 	return NewForcePolicy(matchTag)
	// default:
	// 	return &NilPolicy{}
	// }
}

// Options - additional options when parsing policy
type Options struct {
	MatchTag bool
}

// GetPolicy - policy getter used by Helm config
func GetPolicy(policyName string, options *Options) Policy {

	switch {
	case strings.HasPrefix(policyName, "glob:"):
		p, err := NewGlobPolicy(policyName)
		if err != nil {
			log.WithFields(log.Fields{
				"error":  err,
				"policy": policyName,
			}).Error("failed to parse glob policy, check your deployment configuration")
			return &NilPolicy{}
		}
		return p
	case strings.HasPrefix(policyName, "regexp:"):
		p, err := NewRegexpPolicy(policyName)
		if err != nil {
			log.WithFields(log.Fields{
				"error":  err,
				"policy": policyName,
			}).Error("failed to parse regexp policy, check your deployment configuration")
			return &NilPolicy{}
		}
		return p
	}

	switch policyName {
	case "all", "major", "minor", "patch":
		return ParseSemverPolicy(policyName)
	case "force":
		return NewForcePolicy(options.MatchTag)
	}

	log.Infof("unknown policy '%s'", policyName)

	return &NilPolicy{}
}

// ParseSemverPolicy - parse policy type
func ParseSemverPolicy(policy string) Policy {
	switch policy {
	case "all":
		return NewSemverPolicy(SemverPolicyTypeAll)
	case "major":
		return NewSemverPolicy(SemverPolicyTypeMajor)
	case "minor":
		return NewSemverPolicy(SemverPolicyTypeMinor)
	case "patch":
		return NewSemverPolicy(SemverPolicyTypePatch)
	// case "force":
	// 	return PolicyTypeForce
	default:
		return &NilPolicy{}
	}
}

func getPolicyFromLabels(labels map[string]string) (string, bool) {
	policy, ok := labels[types.KeelPolicyLabel]
	if ok {
		return policy, true
	}
	legacy, ok := labels["keel.observer/policy"]
	return legacy, ok
}

func getMatchTag(labels map[string]string) bool {
	mt, ok := labels[types.KeelForceTagMatchLabel]
	if ok {
		return mt == "true"
	}
	legacyMt, ok := labels[types.KeelForceTagMatchLegacyLabel]
	if ok {
		return legacyMt == "true"
	}

	return false
}