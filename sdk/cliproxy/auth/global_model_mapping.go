package auth

import (
	"regexp"
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	log "github.com/sirupsen/logrus"
)

type globalModelMappingTable struct {
	exact map[string]string
	regex []globalModelRegexMapping
}

type globalModelRegexMapping struct {
	re *regexp.Regexp
	to string
}

func compileGlobalModelMappingTable(mappings []internalconfig.GlobalModelMapping) *globalModelMappingTable {
	table := &globalModelMappingTable{}
	if len(mappings) == 0 {
		return table
	}

	exact := make(map[string]string, len(mappings))
	regexRules := make([]globalModelRegexMapping, 0, len(mappings))
	for i := range mappings {
		from := strings.TrimSpace(mappings[i].From)
		to := strings.TrimSpace(mappings[i].To)
		if from == "" || to == "" {
			continue
		}
		if mappings[i].Regex {
			re, errCompile := regexp.Compile("(?i)" + from)
			if errCompile != nil {
				log.WithFields(log.Fields{
					"from": from,
					"to":   to,
				}).Warnf("global model mapping dropped: invalid regex: %v", errCompile)
				continue
			}
			regexRules = append(regexRules, globalModelRegexMapping{re: re, to: to})
			continue
		}
		exact[strings.ToLower(from)] = to
	}

	if len(exact) > 0 {
		table.exact = exact
	}
	if len(regexRules) > 0 {
		table.regex = regexRules
	}
	return table
}

// SetGlobalModelMappings updates the compiled global request-time model rewrite rules.
func (m *Manager) SetGlobalModelMappings(mappings []internalconfig.GlobalModelMapping) {
	if m == nil {
		return
	}
	table := compileGlobalModelMappingTable(mappings)
	if table == nil {
		table = &globalModelMappingTable{}
	}
	m.globalModelMappings.Store(table)
}

// applyGlobalModelMapping resolves a globally configured request-time model rewrite.
// Rewrites run before OAuth aliases and per-credential aliases.
func (m *Manager) applyGlobalModelMapping(requestedModel string) string {
	resolved := m.resolveGlobalMappedModel(requestedModel)
	if resolved == "" {
		return requestedModel
	}
	return resolved
}

// ResolveRequestModel applies global request-time model rewrites and returns the effective model.
func (m *Manager) ResolveRequestModel(requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ""
	}
	if m == nil {
		return requestedModel
	}
	return m.applyGlobalModelMapping(requestedModel)
}

func (m *Manager) resolveGlobalMappedModel(requestedModel string) string {
	if m == nil {
		return ""
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ""
	}

	requestResult, candidates := modelAliasLookupCandidates(requestedModel)
	if len(candidates) == 0 {
		return ""
	}

	raw := m.globalModelMappings.Load()
	table, _ := raw.(*globalModelMappingTable)
	if table == nil {
		return ""
	}

	for _, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" || table.exact == nil {
			continue
		}
		if target := strings.TrimSpace(table.exact[key]); target != "" {
			return finalizeGlobalMappedModel(target, requestResult)
		}
	}

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, entry := range table.regex {
			if entry.re == nil || !entry.re.MatchString(candidate) {
				continue
			}
			target := strings.TrimSpace(entry.to)
			if target == "" {
				continue
			}
			return finalizeGlobalMappedModel(target, requestResult)
		}
	}

	return ""
}

func finalizeGlobalMappedModel(target string, requestResult thinking.SuffixResult) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	return preserveResolvedModelSuffix(target, requestResult)
}
