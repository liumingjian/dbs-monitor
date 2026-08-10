package alerting

type State string

const (
	OK        State = "OK"
	PENDING   State = "PENDING"
	FIRING    State = "FIRING"
	NO_DATA   State = "NO_DATA"
	RECOVERED State = "RECOVERED"
)

type Evaluation uint8

const (
	Breaching Evaluation = iota
	Recovering
	Stable
	Missing
	MissingIgnored
)

type Snapshot struct {
	State             State
	StateBeforeNoData State
	BreachCount       int
	RecoveryCount     int
	NoDataCount       int
}

func Step(current Snapshot, evaluation Evaluation, triggerCount, recoveryCount int) Snapshot {
	switch evaluation {
	case MissingIgnored:
		current.BreachCount = 0
		current.RecoveryCount = 0
		current.NoDataCount = 0
		return current
	case Missing:
		if current.State == RECOVERED {
			return current
		}
		current.BreachCount = 0
		current.RecoveryCount = 0
		current.NoDataCount++
		if current.NoDataCount >= 2 && current.State != NO_DATA {
			current.StateBeforeNoData = current.State
			current.State = NO_DATA
		}
		return current
	case Breaching, Recovering, Stable:
		current.NoDataCount = 0
		if current.State == NO_DATA {
			current.State = current.StateBeforeNoData
			current.StateBeforeNoData = ""
			return current
		}
	}
	if evaluation == Stable {
		current.BreachCount = 0
		current.RecoveryCount = 0
		if current.State == PENDING {
			current.State = OK
		}
		return current
	}

	if evaluation == Breaching {
		current.RecoveryCount = 0
		switch current.State {
		case OK, RECOVERED:
			current.BreachCount++
			if current.BreachCount >= triggerCount {
				current.State = FIRING
			} else {
				current.State = PENDING
			}
		case PENDING:
			current.BreachCount++
			if current.BreachCount >= triggerCount {
				current.State = FIRING
			}
		}
		return current
	}

	current.BreachCount = 0
	switch current.State {
	case PENDING:
		current.State = OK
	case FIRING:
		current.RecoveryCount++
		if current.RecoveryCount >= recoveryCount {
			current.State = RECOVERED
		}
	}
	return current
}
