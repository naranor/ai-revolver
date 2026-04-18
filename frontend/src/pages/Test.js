import React, { useState, useEffect, useRef } from 'react';
import axios from 'axios';

const availableTools = [
  {
    type: 'function',
    function: {
      name: 'calculator',
      description: 'Evaluates a mathematical expression and returns the result.',
      parameters: {
        type: 'object',
        properties: {
          expression: {
            type: 'string',
            description: "The mathematical expression to evaluate, e.g., '2 + 2' or '254 * 3.14'",
          },
        },
        required: ['expression'],
      },
    },
  },
  {
    type: 'function',
    function: {
      name: 'get_current_time',
      description: 'Returns the current time and date in ISO format.',
      parameters: {
        type: 'object',
        properties: {},
      },
    },
  },
  {
    type: 'function',
    function: {
      name: 'change_ui_color',
      description: 'Changes the theme color of the chat interface.',
      parameters: {
        type: 'object',
        properties: {
          color: {
            type: 'string',
            enum: ['red', 'green', 'blue', 'default'],
            description: 'The color to change the UI to.',
          },
        },
        required: ['color'],
      },
    },
  },
  {
    type: 'function',
    function: {
      name: 'show_notification',
      description: 'Shows a toast notification to the user.',
      parameters: {
        type: 'object',
        properties: {
          message: {
            type: 'string',
            description: 'The message to display in the notification.',
          },
          type: {
            type: 'string',
            enum: ['info', 'success', 'warning', 'error'],
            description: 'The severity/type of the notification.',
          },
        },
        required: ['message', 'type'],
      },
    },
  },
];

function Test() {
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [providers, setProviders] = useState([]);
  const [selectedProvider, setSelectedProvider] = useState('auto');
  const [selectedModel, setSelectedModel] = useState('auto');
  const [loading, setLoading] = useState(false);
  const [configLoading, setConfigLoading] = useState(true);

  // Tactical Toggles
  const [isThinking, setIsThinking] = useState(false);
  const [isReasoning, setIsReasoning] = useState(false);
  const [useTools, setUseTools] = useState(false);

  // Agent State
  const [notifications, setNotifications] = useState([]);
  const [themeColor, setThemeColor] = useState('default');

  const scrollRef = useRef(null);

  useEffect(() => {
    fetchConfig();
  }, []);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages]);

  const fetchConfig = async () => {
    try {
      const response = await axios.get('/api/v1/config');
      setProviders(response.data.providers || []);
      setConfigLoading(false);
    } catch (err) {
      console.error('Error fetching config:', err);
      setConfigLoading(false);
    }
  };

  const getActiveModelData = () => {
    if (selectedProvider === 'auto') return null;
    const p = providers.find((p) => p.name === selectedProvider);
    return p?.models?.find((m) => m.name === selectedModel);
  };

  const activeModel = getActiveModelData();

  const handleProviderChange = (e) => {
    const val = e.target.value;
    setSelectedProvider(val);
    if (val === 'auto') {
      setSelectedModel('auto');
    } else {
      const provider = providers.find((p) => p.name === val);
      if (provider?.models?.length > 0) {
        setSelectedModel(provider.models[0].name);
      } else {
        setSelectedModel('');
      }
    }
  };

  const executeToolCall = (name, argsString) => {
    let args = {};
    try {
      if (argsString) {
        args = JSON.parse(argsString);
      }
    } catch (e) {
      return 'Error parsing arguments: ' + e.message;
    }

    try {
      if (name === 'calculator') {
        // eslint-disable-next-line no-eval
        const result = eval(args.expression);
        return String(result);
      } else if (name === 'get_current_time') {
        return new Date().toISOString();
      } else if (name === 'change_ui_color') {
        setThemeColor(args.color);
        return 'UI color changed to ' + args.color;
      } else if (name === 'show_notification') {
        const newNotif = { message: args.message, type: args.type, id: Date.now() };
        setNotifications((prev) => [...prev, newNotif]);
        setTimeout(() => {
          setNotifications((prev) => prev.filter((n) => n.id !== newNotif.id));
        }, 5000);
        return 'Notification shown to user';
      } else {
        return 'Unknown tool: ' + name;
      }
    } catch (e) {
      return 'Tool execution error: ' + e.message;
    }
  };

  const initiateEngagement = async (e) => {
    if (e) e.preventDefault();
    if (!input.trim() || loading) return;

    const userMessage = { role: 'user', content: input };
    let currentMessages = [...messages, userMessage];
    setMessages(currentMessages);
    setInput('');
    setLoading(true);

    let iterations = 0;
    const MAX_ITERATIONS = 5;

    while (iterations < MAX_ITERATIONS) {
      iterations++;

      const aiMessageIndex = currentMessages.length;
      setMessages((prev) => [
        ...prev,
        { role: 'assistant', content: '', thought: '', loading: true },
      ]);

      try {
        const extraParams = {};
        if (isReasoning) extraParams.reasoning = true;
        if (isThinking) extraParams.thinking = true;

        const requestBody = {
          model: selectedModel,
          stream: true,
          messages: currentMessages,
          ...(Object.keys(extraParams).length > 0 && { extra_params: extraParams }),
        };

        if (useTools && (!activeModel || activeModel.tools !== false)) {
          requestBody.tools = availableTools;
        }

        const response = await fetch('/api/v1/chat/completions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(requestBody),
        });

        if (!response.ok) throw new Error('Engagement Failed');

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let fullContent = '';
        let fullThought = '';
        let toolCalls = {}; // Map of index to tool_call object

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          const chunk = decoder.decode(value);
          const lines = chunk.split('\n');

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              const dataStr = line.slice(6);
              if (dataStr === '[DONE]') continue;
              try {
                const data = JSON.parse(dataStr);
                const delta = data.choices?.[0]?.delta || {};

                // Handle reasoning/thought (varies by provider)
                const thought = delta.reasoning || delta.thought || '';
                const content = delta.content || '';

                if (delta.tool_calls) {
                  for (const tc of delta.tool_calls) {
                    if (!toolCalls[tc.index]) {
                      toolCalls[tc.index] = {
                        id: tc.id || '',
                        type: 'function',
                        function: {
                          name: tc.function?.name || '',
                          arguments: tc.function?.arguments || '',
                        },
                      };
                    } else {
                      if (tc.id) toolCalls[tc.index].id = tc.id;
                      if (tc.function?.name) toolCalls[tc.index].function.name += tc.function.name;
                      if (tc.function?.arguments)
                        toolCalls[tc.index].function.arguments += tc.function.arguments;
                    }
                  }
                }

                const currentContent = fullContent;
                const currentThought = fullThought;

                setMessages((prev) => {
                  const next = [...prev];
                  next[aiMessageIndex] = {
                    role: 'assistant',
                    content: currentContent + content,
                    thought: currentThought + thought,
                    tool_calls: Object.values(toolCalls),
                    loading: false,
                  };
                  return next;
                });

                fullContent = currentContent + content;
                fullThought = currentThought + thought;
              } catch (e) {}
            }
          }
        }

        const finalToolCalls = Object.values(toolCalls);
        if (finalToolCalls.length > 0) {
          // Add the assistant's tool call message to history
          const assistantMessage = {
            role: 'assistant',
            content: fullContent,
            tool_calls: finalToolCalls,
          };
          currentMessages = [...currentMessages, assistantMessage];

          setMessages((prev) => {
            const next = [...prev];
            next[aiMessageIndex].loading = false;
            return next;
          });

          // Execute tools
          for (const tc of finalToolCalls) {
            const result = executeToolCall(tc.function.name, tc.function.arguments);
            const toolMessage = {
              role: 'tool',
              tool_call_id: tc.id,
              name: tc.function.name,
              content: result,
            };
            currentMessages = [...currentMessages, toolMessage];
            setMessages((prev) => [...prev, toolMessage]);
          }
          // Continue loop with new messages
        } else {
          // No tool calls, we are done
          setMessages((prev) => {
            const next = [...prev];
            next[aiMessageIndex].loading = false;
            return next;
          });
          break; // break the while loop
        }
      } catch (err) {
        setMessages((prev) => {
          const next = [...prev];
          next[aiMessageIndex] = {
            role: 'assistant',
            content: `CRITICAL ERROR: ${err.message}`,
            error: true,
          };
          return next;
        });
        break;
      }
    }
    setLoading(false);
  };

  const getThemeStyle = () => {
    switch (themeColor) {
      case 'red':
        return { border: '2px solid var(--danger)', padding: '2px' };
      case 'green':
        return { border: '2px solid var(--success)', padding: '2px' };
      case 'blue':
        return { border: '2px solid var(--accent)', padding: '2px' };
      default:
        return {};
    }
  };

  const getNotifColor = (type) => {
    switch (type) {
      case 'success':
        return 'var(--success)';
      case 'warning':
        return 'var(--warning)';
      case 'error':
        return 'var(--danger)';
      default:
        return 'var(--accent)';
    }
  };

  if (configLoading)
    return (
      <div className="panel">
        <div className="panel-body mono">CALIBRATING...</div>
      </div>
    );

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: 'calc(100vh - 80px)',
        ...getThemeStyle(),
        transition: 'border 0.3s ease',
      }}
    >
      {/* Notifications */}
      <div
        style={{
          position: 'fixed',
          top: '20px',
          right: '20px',
          zIndex: 1000,
          display: 'flex',
          flexDirection: 'column',
          gap: '10px',
        }}
      >
        {notifications.map((n) => (
          <div
            key={n.id}
            style={{
              backgroundColor: 'var(--surface-raised)',
              borderLeft: `4px solid ${getNotifColor(n.type)}`,
              padding: '12px 20px',
              borderRadius: '4px',
              boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
              color: 'var(--text)',
              fontSize: '14px',
              fontFamily: 'var(--font-mono)',
            }}
          >
            {n.message}
          </div>
        ))}
      </div>

      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'flex-end',
          marginBottom: '24px',
        }}
      >
        <div>
          <h1>Ballistic Engagement</h1>
          <div
            style={{ color: 'var(--text-muted)', fontSize: '12px', fontFamily: 'var(--font-mono)' }}
          >
            MODE: {selectedProvider === 'auto' ? 'DYNAMIC ROUTING' : 'MANUAL OVERRIDE'}
          </div>
        </div>

        <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
          <div className="field-group" style={{ marginBottom: 0 }}>
            <label className="field-label">Target Registry</label>
            <select
              className="field-input"
              style={{ width: '180px' }}
              value={selectedProvider}
              onChange={handleProviderChange}
            >
              <option value="auto">AUTO-REVOLVER</option>
              {providers.map((p, i) => (
                <option key={i} value={p.name}>
                  {p.name.toUpperCase()}
                </option>
              ))}
            </select>
          </div>
          <div className="field-group" style={{ marginBottom: 0 }}>
            <label className="field-label">Caliber (Model)</label>
            <select
              className="field-input"
              style={{ width: '220px' }}
              value={selectedModel}
              onChange={(e) => setSelectedModel(e.target.value)}
              disabled={selectedProvider === 'auto'}
            >
              {selectedProvider === 'auto' ? (
                <option value="auto">DYNAMIC</option>
              ) : (
                <>
                  {providers
                    .find((p) => p.name === selectedProvider)
                    ?.models?.map((m, i) => (
                      <option key={i} value={m.name}>
                        {m.name.toUpperCase()}
                      </option>
                    ))}
                </>
              )}
            </select>
          </div>
        </div>
      </div>

      <div
        className="panel"
        style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
      >
        <div
          ref={scrollRef}
          style={{
            flex: 1,
            overflowY: 'auto',
            padding: '24px',
            display: 'flex',
            flexDirection: 'column',
            gap: '20px',
          }}
        >
          {messages.length === 0 && (
            <div
              style={{
                flex: 1,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: 'var(--text-muted)',
                opacity: 0.5,
              }}
            >
              <div className="mono" style={{ textAlign: 'center' }}>
                <div>[ SYSTEM IDLE ]</div>
                <div style={{ fontSize: '10px', marginTop: '8px' }}>
                  AWAITING ENGAGEMENT DIRECTIVES
                </div>
              </div>
            </div>
          )}
          {messages.map((msg, i) => (
            <div
              key={i}
              style={{
                alignSelf: msg.role === 'user' ? 'flex-end' : 'flex-start',
                maxWidth: '85%',
                display: 'flex',
                flexDirection: 'column',
                alignItems: msg.role === 'user' ? 'flex-end' : 'flex-start',
              }}
            >
              <div
                className="mono"
                style={{ fontSize: '10px', color: 'var(--text-muted)', marginBottom: '4px' }}
              >
                {msg.role === 'user'
                  ? 'OPERATOR'
                  : msg.role === 'tool'
                    ? 'TOOL EXECUTION'
                    : 'INTELLIGENCE'}
              </div>
              <div
                style={{
                  backgroundColor:
                    msg.role === 'user'
                      ? 'var(--surface-raised)'
                      : msg.role === 'tool'
                        ? 'transparent'
                        : 'var(--surface-inset)',
                  padding: '12px 16px',
                  borderRadius: 'var(--radius)',
                  border: `1px solid ${msg.error ? 'var(--danger)' : msg.role === 'tool' ? 'var(--border-strong)' : 'var(--border-strong)'}`,
                  color: msg.error
                    ? 'var(--danger)'
                    : msg.role === 'tool'
                      ? 'var(--text-muted)'
                      : 'var(--text)',
                  fontSize: msg.role === 'tool' ? '12px' : '14px',
                  fontFamily: msg.role === 'tool' ? 'var(--font-mono)' : 'inherit',
                  whiteSpace: 'pre-wrap',
                }}
              >
                {msg.thought && (
                  <div
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '11px',
                      color: 'var(--accent)',
                      borderLeft: '2px solid var(--accent)',
                      paddingLeft: '12px',
                      marginBottom: '12px',
                      opacity: 0.8,
                      fontStyle: 'italic',
                    }}
                  >
                    {msg.thought}
                  </div>
                )}
                {msg.tool_calls && msg.tool_calls.length > 0 && (
                  <div
                    style={{
                      marginBottom: msg.content ? '12px' : '0',
                      color: 'var(--accent)',
                      fontSize: '12px',
                      fontFamily: 'var(--font-mono)',
                    }}
                  >
                    {msg.tool_calls.map((tc, idx) => (
                      <div key={idx}>
                        🛠️ Invoking {tc.function?.name}({tc.function?.arguments})
                      </div>
                    ))}
                  </div>
                )}
                {msg.loading ? (
                  <span
                    className="status-indicator status-success"
                    style={{ animation: 'pulse 1s infinite' }}
                  ></span>
                ) : (
                  msg.content
                )}
              </div>
            </div>
          ))}
        </div>

        <div
          className="panel-header"
          style={{
            borderTop: '1px solid var(--border-strong)',
            borderBottom: 'none',
            padding: '16px 24px',
          }}
        >
          <form onSubmit={initiateEngagement} style={{ width: '100%' }}>
            <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
              <div style={{ display: 'flex', gap: '4px' }}>
                <button
                  type="button"
                  disabled={!activeModel?.thinking && selectedProvider !== 'auto'}
                  onClick={() => setIsThinking(!isThinking)}
                  className="btn"
                  style={{
                    fontSize: '9px',
                    borderColor: isThinking ? 'var(--success)' : 'var(--border-strong)',
                    color: isThinking ? 'var(--success)' : 'var(--text-muted)',
                    opacity: !activeModel?.thinking && selectedProvider !== 'auto' ? 0.2 : 1,
                  }}
                >
                  THINK
                </button>
                <button
                  type="button"
                  disabled={!activeModel?.reasoning && selectedProvider !== 'auto'}
                  onClick={() => setIsReasoning(!isReasoning)}
                  className="btn"
                  style={{
                    fontSize: '9px',
                    borderColor: isReasoning ? 'var(--success)' : 'var(--border-strong)',
                    color: isReasoning ? 'var(--success)' : 'var(--text-muted)',
                    opacity: !activeModel?.reasoning && selectedProvider !== 'auto' ? 0.2 : 1,
                  }}
                >
                  REASON
                </button>
                <button
                  type="button"
                  onClick={() => setUseTools(!useTools)}
                  className="btn"
                  style={{
                    fontSize: '9px',
                    borderColor: useTools ? 'var(--success)' : 'var(--border-strong)',
                    color: useTools ? 'var(--success)' : 'var(--text-muted)',
                  }}
                >
                  TOOLS
                </button>
              </div>

              <input
                className="field-input"
                placeholder="ENTER MISSION DIRECTIVE..."
                value={input}
                onChange={(e) => setInput(e.target.value)}
                disabled={loading}
                style={{ flex: 1, height: '40px' }}
              />
              <button
                type="submit"
                className="btn btn-primary"
                style={{ height: '40px', padding: '0 24px' }}
                disabled={loading || !input.trim()}
              >
                {loading ? 'EXECUTING...' : 'FIRE'}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}

export default Test;
