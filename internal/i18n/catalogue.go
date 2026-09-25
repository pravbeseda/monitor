package i18n

// catalogue is keyed by English identifiers, so a missing translation is visible rather
// than silently English.
var catalogue = map[string]map[Locale]string{
	"page.title":           {English: "Monitor", Russian: "Монитор"},
	"page.empty":           {English: "No node has reported yet", Russian: "Ни один узел ещё не отчитался"},
	"page.stalled":         {English: "Not refreshed: the hub is not answering", Russian: "Не обновляется: хаб не отвечает"},
	"node.last_seen":       {English: "Last seen", Russian: "Последний отчёт"},
	"node.no_values":       {English: "No measurements yet", Russian: "Измерений ещё нет"},
	"node.no_current":      {English: "No current measurements", Russian: "Актуальных измерений нет"},
	"node.silent":          {English: "silent", Russian: "молчит"},
	"node.unwatched":       {English: "%d series without a threshold", Russian: "серий без порога: %d"},
	"table.metric":         {English: "Metric", Russian: "Метрика"},
	"table.volume":         {English: "Volume", Russian: "Том"},
	"table.free":           {English: "Value", Russian: "Значение"},
	"table.level":          {English: "Level", Russian: "Уровень"},
	"table.collected":      {English: "Collected", Russian: "Собрано"},
	"table.set":            {English: "set", Russian: "настроить"},
	"label.removable":      {English: "removable", Russian: "съёмный"},
	"value.stale":          {English: "no fresh data", Russian: "нет свежих данных"},
	"value.unusual":        {English: "unusual, usually %s", Russian: "необычно, обычно %s"},
	"board.title":          {English: "Mission control", Russian: "Центр управления"},
	"timeline.title":       {English: "Timeline", Russian: "Лента"},
	"timeline.now":         {English: "Now", Russian: "Сейчас"},
	"timeline.lanes":       {English: "Last 24 hours", Russian: "Последние 24 часа"},
	"timeline.changes":     {English: "What changed", Russian: "Что менялось"},
	"timeline.no_changes":  {English: "No level has changed yet", Russian: "Уровни ещё не менялись"},
	"timeline.fell_silent": {English: "fell silent", Russian: "замолчал"},
	"timeline.no_report":   {English: "no report for %s", Russian: "нет отчётов %s"},
	"timeline.reporting":   {English: "reporting again", Russian: "снова на связи"},
	"cell.silent":          {English: "silent", Russian: "молчит"},
	"cell.reporting":       {English: "reporting, nothing watched", Russian: "на связи, без порогов"},
	"day.today":            {English: "Today", Russian: "Сегодня"},
	"day.yesterday":        {English: "Yesterday", Russian: "Вчера"},
	"board.all_well":       {English: "All is well", Russian: "Всё в порядке"},
	"board.nothing_past":   {English: "Nothing past a threshold", Russian: "Пороги не превышены"},
	"board.nothing_judged": {English: "Nothing is judged yet", Russian: "Пока ничего не оценивается"},
	"board.since":          {English: "since %s", Russian: "с %s"},
	"board.usually":        {English: "usually %s", Russian: "обычно %s"},
	"board.stale":          {English: "no fresh data since %s", Russian: "нет свежих данных с %s"},
	"board.silent":         {English: "silent, last seen %s", Russian: "молчит, последний отчёт %s"},
	"board.more":           {English: "more unusual series: %d", Russian: "ещё необычных серий: %d"},
	"page.all_series":      {English: "All series", Russian: "Все серии"},
	"page.nothing_watched": {
		English: "Nothing here is being judged yet: open a series and set a threshold for it.",
		Russian: "Здесь пока ничего не оценивается: откройте серию и задайте порог.",
	},

	"error.storage":        {English: "The panel cannot read its data right now", Russian: "Панель сейчас не может прочитать свои данные"},
	"error.method":         {English: "method not allowed", Russian: "метод не поддерживается"},
	"error.query":          {English: "this address does not name one series", Russian: "этот адрес не называет одну серию"},
	"error.unknown_series": {English: "no such series has reported", Russian: "такая серия не приходила"},
	"error.origin":         {English: "this save did not come from this page; reload it and try again", Russian: "сохранение пришло не с этой страницы; перезагрузите её и повторите"},

	"level.ok":       {English: "ok", Russian: "норма"},
	"level.warning":  {English: "warning", Russian: "предупреждение"},
	"level.critical": {English: "critical", Russian: "критично"},

	"notify.changed":  {English: "%s: %s (was %s since %s)", Russian: "%s: %s (было %s с %s)"},
	"notify.standing": {English: "%s: still %s since %s", Russian: "%s: по-прежнему %s с %s"},
	"notify.reading":  {English: "%s is %s", Russian: "%s — %s"},
	"notify.silent":   {English: "no report", Russian: "нет отчётов"},
	"digest.title":    {English: "Daily digest", Russian: "Ежедневная сводка"},
	"digest.nothing_watched": {
		English: "Nothing on this hub is being judged: open a series and set a threshold for it.",
		Russian: "На этом хабе ничего не оценивается: откройте серию и задайте порог.",
	},
	"digest.unwatched": {
		English: "%d series have no threshold and are not judged.",
		Russian: "серий без порога, они не оцениваются: %d",
	},

	"threshold.title":           {English: "What this series is judged by", Russian: "Чем оценивается эта серия"},
	"threshold.direction":       {English: "Alert when the value is", Russian: "Тревога, когда значение"},
	"threshold.below":           {English: "below the threshold", Russian: "ниже порога"},
	"threshold.above":           {English: "above the threshold", Russian: "выше порога"},
	"threshold.unusual":         {English: "When this series is unusual", Russian: "Когда серия необычна"},
	"threshold.unusual_show":    {English: "show it on mission control", Russian: "показывать в центре управления"},
	"threshold.unusual_exclude": {English: "never show it as unusual", Russian: "никогда не показывать как необычную"},
	"threshold.bad_switch":      {English: "Say whether this series may be shown as unusual.", Russian: "Укажите, можно ли показывать эту серию как необычную."},
	"threshold.save":            {English: "Save", Russian: "Сохранить"},
	"threshold.back":            {English: "History of this series", Russian: "История этой серии"},
	"threshold.unwatched": {
		English: "Nothing is set, so this series has no level and never alerts.",
		Russian: "Ничего не задано: у серии нет уровня и она не шлёт тревог.",
	},
	"threshold.detached": {
		English: "The configuration no longer names this node, so the series is not judged.",
		Russian: "Эта нода больше не описана в конфигурации, поэтому серия не оценивается.",
	},
	"threshold.unreadable": {
		English: "The stored configuration could not be read; saving replaces it.",
		Russian: "Сохранённую настройку не удалось прочитать; сохранение заменит её.",
	},
	"threshold.bad_direction": {English: "Choose one direction.", Russian: "Выберите направление."},
	"threshold.bad_warning": {
		English: "The warning value is not a number.",
		Russian: "Значение предупреждения не число.",
	},
	"threshold.bad_critical": {
		English: "The critical value is not a number.",
		Russian: "Критическое значение не число.",
	},
	"threshold.unordered": {
		English: "Critical must be strictly beyond warning in the chosen direction.",
		Russian: "Критическое значение должно быть строго за предупреждением в выбранном направлении.",
	},

	// The unit a metric id declares, as the form names the field's unit …
	"unit.bytes":    {English: "bytes, e.g. 10GB", Russian: "байты, например 10GB"},
	"unit.percent":  {English: "percent", Russian: "проценты"},
	"unit.duration": {English: "seconds", Russian: "секунды"},
	"unit.number":   {English: "number", Russian: "число"},
	// … and the suffixes a rendered duration is written with.
	"unit.seconds": {English: "s", Russian: "с"},
	"unit.minutes": {English: "min", Russian: "мин"},
	"unit.hours":   {English: "h", Russian: "ч"},
	"unit.days":    {English: "d", Russian: "д"},

	"history.title":   {English: "History", Russian: "История"},
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
