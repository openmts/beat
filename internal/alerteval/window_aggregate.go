package alerteval

// windowGauge 的前置边界样本只证明覆盖，不参与窗口内的样本均值。
func windowGauge(aggregation string, points []EvidencePoint) (float64, error) {
	value := points[0].Value
	for index, point := range points[1:] {
		switch aggregation {
		case "last":
			value = point.Value
		case "min":
			value = min(value, point.Value)
		case "max":
			value = max(value, point.Value)
		case "avg":
			count := float64(index + 2)
			value = value*((count-1)/count) + point.Value/count
		case "sum":
			value += point.Value
		}
	}
	if !finite(value) {
		return 0, ErrInvalidObservation
	}
	return value, nil
}

// windowCounterRate 按相邻真实样本计算增量；跨越左边界的区间按重叠时长计入。
func windowCounterRate(window EvidenceWindow, points []EvidencePoint) (float64, error) {
	for _, point := range points {
		if point.Value < 0 {
			return 0, ErrInvalidObservation
		}
	}
	var rate float64
	span := window.To.Sub(window.From).Seconds()
	for index := 1; index < len(points); index++ {
		previous, current := points[index-1], points[index]
		delta := current.Value - previous.Value
		if delta < 0 {
			delta = current.Value
		}
		start := previous.At
		if start.Before(window.From) {
			start = window.From
		}
		overlap := current.At.Sub(start).Seconds()
		interval := current.At.Sub(previous.At).Seconds()
		rate += (delta * (overlap / interval)) / span
	}
	if !finite(rate) {
		return 0, ErrInvalidObservation
	}
	return rate, nil
}
