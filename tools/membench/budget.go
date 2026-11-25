package main

type budget struct {
	tokens int64
	usd    float64
}

type spend struct {
	tokens int64
	usd    float64
}

func (s spend) plus(other spend) spend {
	return spend{tokens: s.tokens + other.tokens, usd: s.usd + other.usd}
}

func (b budget) reached(s spend) bool {
	return (b.tokens > 0 && s.tokens >= b.tokens) || (b.usd > 0 && s.usd >= b.usd)
}

type meter struct {
	limit   budget
	reserve spend
	spent   spend
	widest  spend
}

func (m *meter) projected() spend {
	return spend{
		tokens: max(m.reserve.tokens, m.widest.tokens),
		usd:    max(m.reserve.usd, m.widest.usd),
	}
}

func (m *meter) mayStart() bool {
	return !m.limit.reached(m.spent.plus(m.projected()))
}

func (m *meter) charge(call spend) {
	m.spent = m.spent.plus(call)
	m.widest = spend{tokens: max(m.widest.tokens, call.tokens), usd: max(m.widest.usd, call.usd)}
}
