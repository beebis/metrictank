package tagquery

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/raintank/schema"
)

type expressionMatch struct {
	expressionCommonRe
}

func (e *expressionMatch) GetDefaultDecision() FilterDecision {
	// if the pattern matches "" (f.e. "tag=~.*) then a metric which
	// does not have the tag "tag" at all should also be part of the
	// result set
	// docs: https://graphite.readthedocs.io/en/latest/tags.html
	// > Any tag spec that matches an empty value is considered to
	// > match series that don’t have that tag
	if e.matchesEmpty {
		return Pass
	}
	return Fail
}

func (e *expressionMatch) GetOperator() ExpressionOperator {
	return MATCH
}

func (e *expressionMatch) HasRe() bool {
	return true
}

func (e *expressionMatch) RequiresNonEmptyValue() bool {
	return !e.matchesEmpty
}

func (e *expressionMatch) ValuePasses(value string) bool {
	return e.valueRe.MatchString(value)
}

func (e *expressionMatch) GetMetricDefinitionFilter(_ IdTagLookup) MetricDefinitionFilter {
	if e.key == "name" {
		if e.value == "" {
			// silly query, always fails
			return func(id schema.MKey, name string, tags []string) FilterDecision { return Fail }
		}
		return func(id schema.MKey, name string, tags []string) FilterDecision {
			if e.valueRe.MatchString(schema.SanitizeNameAsTagValue(name)) {
				return Pass
			} else {
				return Fail
			}
		}
	}

	resultIfTagIsAbsent := None
	if !metaTagSupport {
		resultIfTagIsAbsent = Fail
	}

	prefix := e.key + "="
	var matchCache, missCache sync.Map
	var currentMatchCacheSize, currentMissCacheSize int32
	return func(id schema.MKey, name string, tags []string) FilterDecision {
		for _, tag := range tags {
			if !strings.HasPrefix(tag, prefix) {
				continue
			}

			value := tag[len(prefix):]

			// reduce regex matching by looking up cached non-matches
			if _, ok := missCache.Load(value); ok {
				return Fail
			}

			// reduce regex matching by looking up cached matches
			if _, ok := matchCache.Load(value); ok {
				return Pass
			}

			if e.valueRe.MatchString(value) {
				if atomic.LoadInt32(&currentMatchCacheSize) < int32(matchCacheSize) {
					matchCache.Store(value, struct{}{})
					atomic.AddInt32(&currentMatchCacheSize, 1)
				}
				return Pass
			} else {
				if atomic.LoadInt32(&currentMissCacheSize) < int32(matchCacheSize) {
					missCache.Store(value, struct{}{})
					atomic.AddInt32(&currentMissCacheSize, 1)
				}
				return Fail
			}
		}

		return resultIfTagIsAbsent
	}
}

func (e *expressionMatch) StringIntoBuilder(builder *strings.Builder) {
	builder.WriteString(e.key)
	builder.WriteString("=~")
	builder.WriteString(e.value)
}