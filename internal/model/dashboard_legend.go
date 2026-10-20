package model

// dashboardLegendPlacements 是图例位置取值：bottom 与 Grafana 的 Bottom 一致，right 把图例放到绘图区右侧。
// Grafana 的 top 等其它写法这里没有实现，按未知取值拒绝而不是静默当成 bottom。
var dashboardLegendPlacements = []string{"bottom", "right"}
