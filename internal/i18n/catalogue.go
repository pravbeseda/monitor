package i18n

// catalogue is keyed by English identifiers, so a missing translation is visible rather
// than silently English.
var catalogue = map[string]map[Locale]string{
	"page.title":      {English: "Monitor", Russian: "Монитор"},
	"page.nodes":      {English: "Nodes", Russian: "Узлы"},
	"page.empty":      {English: "No node has reported yet", Russian: "Ни один узел ещё не отчитался"},
	"page.stalled":    {English: "Not refreshed: the hub is not answering", Russian: "Не обновляется: хаб не отвечает"},
	"node.last_seen":  {English: "Last seen", Russian: "Последний отчёт"},
	"node.no_values":  {English: "No measurements yet", Russian: "Измерений ещё нет"},
	"node.no_current": {English: "No current measurements", Russian: "Актуальных измерений нет"},
	"node.silent":     {English: "silent", Russian: "молчит"},
	"table.metric":    {English: "Metric", Russian: "Метрика"},
	"table.volume":    {English: "Volume", Russian: "Том"},
	"table.free":      {English: "Value", Russian: "Значение"},
	"table.level":     {English: "Level", Russian: "Уровень"},
	"table.collected": {English: "Collected", Russian: "Собрано"},
	"label.removable": {English: "removable", Russian: "съёмный"},
	"value.stale":     {English: "no fresh data", Russian: "нет свежих данных"},
	"error.storage":   {English: "The panel cannot read its data right now", Russian: "Панель сейчас не может прочитать свои данные"},

	"level.ok":       {English: "ok", Russian: "норма"},
	"level.warning":  {English: "warning", Russian: "предупреждение"},
	"level.critical": {English: "critical", Russian: "критично"},

	"notify.changed":  {English: "%s: %s (was %s since %s)", Russian: "%s: %s (было %s с %s)"},
	"notify.standing": {English: "%s: still %s since %s", Russian: "%s: по-прежнему %s с %s"},
	"notify.readings": {English: "%s free, %s", Russian: "свободно %s, %s"},
	"notify.silent":   {English: "no report", Russian: "нет отчётов"},
	"digest.title":    {English: "Daily digest", Russian: "Ежедневная сводка"},

	"history.title":   {English: "History", Russian: "История"},
	"history.index":   {English: "All nodes", Russian: "Все узлы"},
	"history.window":  {English: "Window", Russian: "Окно"},
	"history.latest":  {English: "Latest", Russian: "Последнее значение"},
	"history.empty":   {English: "No data for this window", Russian: "Нет данных за это окно"},
	"history.several": {English: "Several series answer this query", Russian: "Этому запросу отвечает несколько серий"},

	"history.error.metric": {
		English: "A history query needs one metric",
		Russian: "Запросу истории нужна ровно одна метрика",
	},
	"history.error.window": {
		English: "The window is a whole number of minutes, hours or days, from 1m to 365d",
		Russian: "Окно задаётся целым числом минут, часов или дней, от 1m до 365d",
	},
	"history.error.empty_parameter": {
		English: "A parameter of this query carries no value",
		Russian: "Параметр запроса остался без значения",
	},
	"history.error.unknown_parameter": {
		English: "This query carries a parameter the panel does not know",
		Russian: "В запросе есть параметр, которого панель не знает",
	},
	"history.error.repeated_parameter": {
		English: "A parameter of this query is given more than once",
		Russian: "Параметр запроса задан больше одного раза",
	},
	"history.error.too_many_series": {
		English: "This query answers with more series than one page holds",
		Russian: "Этому запросу отвечает больше серий, чем помещается на страницу",
	},
}
