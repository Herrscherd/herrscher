package selftools

const (
	typeObject  = "object"
	typeString  = "string"
	typeBoolean = "boolean"
	typeArray   = "array"
)

// Tool is one MCP tool as tools/list declares it.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type property struct {
	Type        any                 `json:"type"`
	Description string              `json:"description,omitempty"`
	Items       *property           `json:"items,omitempty"`
	Properties  map[string]property `json:"properties,omitempty"`
}

type inputSchema struct {
	Type                 string              `json:"type"`
	Properties           map[string]property `json:"properties"`
	Required             []string            `json:"required,omitempty"`
	AdditionalProperties bool                `json:"additionalProperties"`
}

func scalarType() []string {
	return []string{typeString, "number", typeBoolean}
}

func jsonProperty() property {
	return property{Type: typeBoolean, Description: "Ask the daemon for machine JSON instead of text, when the verb has a JSON form."}
}

func restProperty() property {
	return property{
		Type:        typeArray,
		Description: "Positional arguments, for the verbs that take one (an approval id, a session name given bare).",
		Items:       &property{Type: typeString},
	}
}

func commandsToolDef() Tool {
	return Tool{
		Name: commandsTool,
		Description: "List every verb the herrscher daemon knows, with its help and parameters, as JSON. " +
			"Pass family (session, agent, schedule, approve, memory, config, setup, issue, ...) to narrow the list.",
		InputSchema: inputSchema{
			Type: typeObject,
			Properties: map[string]property{
				familyArg: {Type: typeString, Description: "The first word of the verbs to list."},
			},
			AdditionalProperties: false,
		},
	}
}

func runToolDef() Tool {
	return Tool{
		Name: runTool,
		Description: "Run one herrscher verb on the daemon of this session, as the session. " +
			"command is the verb path (\"session list\", \"schedule add\"), args its named parameters as herrscher_commands lists them.",
		InputSchema: inputSchema{
			Type: typeObject,
			Properties: map[string]property{
				commandArg: {Type: typeString, Description: "The verb path, words separated by spaces."},
				argsArg:    {Type: typeObject, Description: "The named parameters, each a string, a number or a boolean."},
				restArg:    restProperty(),
				jsonArg:    jsonProperty(),
			},
			Required:             []string{commandArg},
			AdditionalProperties: false,
		},
	}
}

func verbTool(declared spec) Tool {
	properties := map[string]property{jsonArg: jsonProperty(), restArg: restProperty()}
	var required []string
	for _, declaredParam := range declared.Params {
		properties[declaredParam.Name] = property{Type: scalarType(), Description: declaredParam.Help}
		if declaredParam.Required {
			required = append(required, declaredParam.Name)
		}
	}
	description := declared.Help
	if description == "" {
		description = "herrscher " + declared.verb()
	}
	return Tool{
		Name:        declared.toolName(),
		Description: description,
		InputSchema: inputSchema{Type: typeObject, Properties: properties, Required: required, AdditionalProperties: false},
	}
}

// BooleanTool declares a tool whose arguments are a few optional booleans.
func BooleanTool(name, description string, flags map[string]string) Tool {
	properties := make(map[string]property, len(flags))
	for flagName, help := range flags {
		properties[flagName] = property{Type: typeBoolean, Description: help}
	}
	return Tool{Name: name, Description: description, InputSchema: inputSchema{Type: typeObject, Properties: properties, AdditionalProperties: false}}
}

// Flag reads an optional boolean argument of a tool call.
func Flag(args map[string]any, name string) bool {
	value, ok := args[name].(bool)
	return ok && value
}
