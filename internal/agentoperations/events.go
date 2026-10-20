package agentoperations

type Event struct {
	AttemptID        string
	State            string
	Reason           string
	DurationMS       int64
	OutputBytes      int
	TransferredBytes int64
}

func (runner *Runner) emit(record journal) {
	if runner.onEvent == nil {
		return
	}
	event := Event{AttemptID: record.Lease.AttemptID, State: record.State}
	if record.Result != nil {
		event.State, event.Reason = record.Result.State, record.Result.Reason
		event.OutputBytes = len(record.Result.Stdout) + len(record.Result.Stderr)
		event.DurationMS = max(int64(0), record.Result.FinishedAt-record.Result.StartedAt)
	}
	if record.Transfer != nil {
		event.TransferredBytes = transferredBytes(record)
	}
	runner.onEvent(event)
}

func transferredBytes(record journal) int64 {
	if record.Transfer.Source != nil && record.Transfer.Phase == "finalized" {
		return record.Transfer.Source.Size
	}
	if record.Lease.Payload.File != nil && record.Transfer.Phase == "published" {
		return record.Lease.Payload.File.Size
	}
	return 0
}
