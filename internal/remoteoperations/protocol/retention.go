package protocol

// RetentionPolicy 的零值采用有界默认值；任务与输出以终态时间起算。
type RetentionPolicy struct {
	TaskDays      int `json:"task_days,omitempty"`
	OutputDays    int `json:"output_days,omitempty"`
	ArtifactHours int `json:"artifact_hours,omitempty"`
	QueueHours    int `json:"queue_hours,omitempty"`
	UploadHours   int `json:"upload_hours,omitempty"`
}

func (policy Policy) RetentionLimits() RetentionPolicy {
	value := RetentionPolicy{}
	if policy.Retention != nil {
		value = *policy.Retention
	}
	if value.TaskDays == 0 {
		value.TaskDays = 30
	}
	if value.OutputDays == 0 {
		value.OutputDays = min(7, value.TaskDays)
	}
	if value.ArtifactHours == 0 {
		value.ArtifactHours = 24
	}
	if value.QueueHours == 0 {
		value.QueueHours = 24
	}
	if value.UploadHours == 0 {
		value.UploadHours = 6
	}
	return value
}

func (policy Policy) validateRetention() error {
	value := policy.RetentionLimits()
	for _, limit := range []struct{ value, maximum int }{{value.TaskDays, 90}, {value.OutputDays, min(30, value.TaskDays)}, {value.ArtifactHours, 24}, {value.QueueHours, 24}, {value.UploadHours, 6}} {
		if limit.value < 1 || limit.value > limit.maximum {
			return ErrInvalid
		}
	}
	return nil
}
