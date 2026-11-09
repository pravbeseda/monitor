# All series and settings: what the prototype settled

The `/debug` page and the settings form read as a pile of information. A throwaway HTML
prototype, on synthetic data, put three layouts and four places for the form side by side.

Popular monitoring tools were checked first. Datadog opens a host's detail in a side panel
from its row, Zabbix's latest data is a filterable list with item actions on the item name,
and NN/g keeps modal dialogs for interruptions that matter while Carbon limits toasts to
brief feedback, dismissible and gone after about five seconds.

The prototype's recommendation was clean matrices with a side panel. The reader chose two
panes and a modal dialog instead, and asked for the mark to stand before the number, which
also keeps the numbers of a column right-aligned. What was rejected is in
[0059](../decisions/0059-all-series-is-two-panes-and-settings-a-dialog.md).

Two things changed between the prototype and the build:

- The prototype filtered inside the chosen group, which reads "nothing found" while the left
  pane shows the problem elsewhere. A filter now spans every node.
- The prototype's live "level now" line would have put the judging rule in the browser.
  The chart's lines move as a value is typed, its units supplied by the page; judging and
  refusing a value stay the server's.
