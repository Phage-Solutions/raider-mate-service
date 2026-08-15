package api

// Link is one HATEOAS transition: where to go and what verb to use.
type Link struct {
	Href   string `json:"href"`
	Method string `json:"method,omitempty"`
}

// Links is a named set of transitions, embedded in a response as "_links". This is
// the one link-building mechanism the whole API uses (design.md is explicit that it
// is the sole justified up-front abstraction, and forbids HAL and JSON:API): every
// resource response builds one of these by calling add conditionally on state and
// actor, never by hardcoding a link per endpoint.
type Links map[string]Link

// add adds name only when include is true. The absence of a link is the
// authorization or state answer: a caller who cannot see a transition cannot
// perform it, without a separate permission check on the client side.
func (l Links) add(include bool, name, href, method string) {
	if !include {
		return
	}
	l[name] = Link{Href: href, Method: method}
}
