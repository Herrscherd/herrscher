# Référence des actions de Claude Code (skill keybindings-help, recopiée)

Contextes : Global, Chat, Autocomplete, Confirmation, Help, Transcript, HistorySearch, Task, ThemePicker, Settings, Tabs, Attachments, Footer, AbovePrompt, AbovePromptInput, AbovePromptSelect, Pane, PaneField, MessageSelector, DiffDialog, DiffPanel, ModelPicker, EffortSlider, Select, Plugin, Scroll, Agents.

Touches non reliables : ctrl+c, ctrl+d, ctrl+m, ctrl+[, ctrl+i, ctrl+h, capslock. Accords séparés par une espace, délai 3 s.

| Action | Touches par défaut | Contexte |
| --- | --- | --- |
| app:interrupt | ctrl+c | Global |
| app:exit | ctrl+d | Global |
| app:toggleTodos | ctrl+t | Global |
| app:toggleTranscript | ctrl+o | Global |
| app:toggleBrief | ctrl+shift+b | Global |
| app:toggleReplTab | | Global |
| app:toggleDiffNoiseFilter | | Global |
| app:diffFileListUp | ctrl+up, meta+up | Global |
| app:diffFileListDown | ctrl+down, meta+down | Global |
| app:toggleDiffPreSession | | Global |
| app:cycleDiffBase | ctrl+x b | DiffPanel |
| app:toggleTerminal | | Global |
| app:redraw | | Global |
| app:openArtifact | ctrl+] | Global |
| history:search | ctrl+r | Global |
| history:previous | up | Chat |
| history:next | down | Chat |
| chat:cancel | escape | Chat |
| chat:killAgents | ctrl+x ctrl+k | Chat |
| chat:cycleMode | shift+tab | Chat |
| chat:modelPicker | meta+p | Chat |
| chat:fastMode | meta+o | Chat |
| chat:defaultToNewerModel | ctrl+y | Chat |
| chat:thinkingToggle | meta+t | Chat |
| chat:workflowKeywordToggle | meta+w | Chat |
| chat:submit | enter | Chat |
| chat:queueSubmit | ctrl+x enter | Chat |
| chat:sendNow | ctrl+x ctrl+s, ctrl+enter | Chat |
| chat:newline | ctrl+j | Chat |
| chat:undo | ctrl+_, ctrl+-, ctrl+shift+-, ctrl+shift+_ | Chat |
| chat:externalEditor | ctrl+x ctrl+e, ctrl+g | Chat |
| chat:stash | ctrl+s | Chat |
| chat:imagePaste | ctrl+v | Chat |
| chat:clearInput | ctrl+l | Chat |
| chat:clearScreen | cmd+k | Chat |
| autocomplete:accept | tab | Autocomplete |
| autocomplete:dismiss | escape | Autocomplete |
| autocomplete:previous | up | Autocomplete |
| autocomplete:next | down | Autocomplete |
| confirm:yes | enter | Confirmation |
| confirm:no | escape | Confirmation, Settings |
| confirm:previous | up | Confirmation |
| confirm:next | down | Confirmation |
| confirm:nextField | tab | Confirmation |
| confirm:previousField | | Confirmation |
| confirm:cycleMode | shift+tab | Confirmation |
| confirm:toggle | space | Confirmation |
| tabs:next | tab, right | Tabs |
| tabs:previous | shift+tab, left | Tabs |
| transcript:toggleShowAll | ctrl+e | Transcript |
| transcript:exit | ctrl+c, escape, q | Transcript |
| historySearch:next | ctrl+r | HistorySearch |
| historySearch:accept | escape, tab | HistorySearch |
| historySearch:cancel | ctrl+c | HistorySearch |
| historySearch:execute | enter | HistorySearch |
| historySearch:cycleScope | ctrl+s | HistorySearch |
| task:background | ctrl+x ctrl+b, ctrl+b | Task |
| theme:toggleSyntaxHighlighting | ctrl+t | ThemePicker |
| theme:editCustom | ctrl+e | ThemePicker |
| help:dismiss | escape | Help |
| attachments:next | right | Attachments |
| attachments:previous | left | Attachments |
| attachments:remove | backspace, delete | Attachments |
| attachments:exit | down, escape | Attachments |
| footer:up | up, ctrl+p | Footer |
| footer:down | down, ctrl+n | Footer |
| footer:next | right | Footer |
| footer:previous | left | Footer |
| footer:openSelected | enter | Footer |
| footer:clearSelection | escape | Footer |
| footer:close | x | Footer |
| footer:dismiss | | Footer |
| abovePrompt:toggle | ctrl+x ctrl+a | Chat |
| abovePrompt:focus | ctrl+x tab | Chat |
| abovePrompt:next | tab, right | AbovePrompt, AbovePromptInput, AbovePromptSelect, PaneField |
| abovePrompt:previous | shift+tab, left | AbovePrompt, AbovePromptInput, AbovePromptSelect, PaneField |
| abovePrompt:press | enter, space | AbovePrompt, AbovePromptInput, AbovePromptSelect, PaneField |
| abovePrompt:leave | escape | AbovePrompt, AbovePromptInput, AbovePromptSelect, PaneField |
| abovePrompt:highlightNext | down | AbovePromptSelect |
| abovePrompt:highlightPrevious | up | AbovePromptSelect |
| pane:scrollUp | up | AbovePrompt, Pane |
| pane:scrollDown | down | AbovePrompt, Pane |
| pane:pageUp | pageup | AbovePrompt, Pane |
| pane:pageDown | pagedown | AbovePrompt, Pane |
| pane:top | home | AbovePrompt, Pane |
| pane:bottom | end | AbovePrompt, Pane |
| pane:grow | ctrl+x left, ctrl+x up | Pane |
| pane:shrink | ctrl+x right, ctrl+x down | Pane |
| pane:close | ctrl+x x | Pane |
| pane:next | | Pane |
| pane:previous | | Pane |
| diff:dismiss | escape | DiffDialog |
| diff:previousSource | left | DiffDialog |
| diff:nextSource | right | DiffDialog |
| diff:back | | DiffDialog |
| diff:previousFile | up, k | DiffDialog |
| diff:nextFile | down, j | DiffDialog |
| modelPicker:decreaseEffort | left | ModelPicker |
| modelPicker:increaseEffort | right | ModelPicker |
| modelPicker:thisSessionOnly | s | ModelPicker |
| effortSlider:decreaseEffort | left | EffortSlider |
| effortSlider:increaseEffort | right | EffortSlider |
| effortSlider:toggleUltracode | tab | EffortSlider |
| effortSlider:thisSessionOnly | s | EffortSlider |
| select:next | down, j, ctrl+n | Select, Settings |
| select:previous | up, k, ctrl+p | Select, Settings |
| select:pageUp | pageup | Select, Settings |
| select:pageDown | pagedown | Select, Settings |
| select:first | home | Select, Settings |
| select:last | end | Select, Settings |
| select:accept | space, enter | Select, Settings |
| select:cancel | escape | Select |
| plugin:toggle | space | Plugin |
| plugin:install | i | Plugin |
| plugin:favorite | f | Plugin |
| plugin:cycleMarketplace | ctrl+s | Plugin |
| permission:toggleDebug | | Confirmation |
| settings:search | / | Settings |
| settings:retry | r | Settings |
| settings:periodDay | d | Settings |
| settings:periodWeek | w | Settings |
| settings:fixUsage | f | Settings |
| settings:sortByTokens | t | Settings |
| voice:pushToTalk | space | Chat |
| scroll:pageUp | pageup | Scroll, Transcript |
| scroll:pageDown | pagedown | Scroll, Transcript |
| scroll:lineUp | ctrl+p, k, up, wheelup | Scroll, Transcript |
| scroll:lineDown | ctrl+n, j, down, wheeldown | Scroll, Transcript |
| scroll:top | g, home, ctrl+home | Scroll, Transcript |
| scroll:bottom | shift+g, end, ctrl+end | Scroll, Transcript |
| scroll:halfPageUp | ctrl+u | Scroll, Transcript, Settings |
| scroll:halfPageDown | ctrl+d | Scroll, Transcript, Settings |
| scroll:fullPageUp | ctrl+b, b, shift+space | Scroll, Transcript |
| scroll:fullPageDown | ctrl+f, space | Scroll, Transcript |
| selection:copy | ctrl+shift+c, cmd+c | Scroll |
| selection:clear | | Scroll |
| selection:extendLeft | shift+left | Scroll |
| selection:extendRight | shift+right | Scroll |
| selection:extendUp | shift+up | Scroll |
| selection:extendDown | shift+down | Scroll |
| selection:extendLineStart | shift+home | Scroll |
| selection:extendLineEnd | shift+end | Scroll |
| agents:switchView | ctrl+s | Agents |
| agents:togglePin | ctrl+t | Agents |
| agents:find | ctrl+f | Agents |
| agents:nextGroup | ctrl+down, meta+down | Agents |
| agents:previousGroup | ctrl+up, meta+up | Agents |
| agents:rename | ctrl+r | Agents |
