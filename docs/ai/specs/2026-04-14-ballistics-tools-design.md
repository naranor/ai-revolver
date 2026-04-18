# Design Spec: Client-Side Agent Loop (Ballistics Tools)

## 1. Overview
The goal is to upgrade the "Ballistics Engagement" page (`frontend/src/pages/Test.js`) from a simple chat interface into a fully functional AI Agent sandbox. This will be achieved by implementing a client-side agent loop capable of handling `tool_calls` (Function Calling) directly within the React frontend, using a set of safe, browser-native tools.

## 2. Architecture (Agent Loop)
The core logic resides in the `initiateEngagement` function. It will be refactored into a recursive or loop-based structure:

1. **Request Phase:** Append the user message and the defined `tools` schema to the request payload. Send the request to `/api/v1/chat/completions` with `stream: true`.
2. **Stream Processing Phase:** Parse the incoming Server-Sent Events (SSE).
   - If `delta.content` is present, stream the text to the UI.
   - If `delta.tool_calls` are present, accumulate the function names and JSON arguments.
3. **Execution Phase (End of Stream):**
   - If no tool calls were made, the loop terminates.
   - If tool calls were made:
     - Pause UI streaming.
     - Parse the accumulated JSON arguments.
     - Execute the corresponding JavaScript function locally.
     - Append the results to the message history with `role: "tool"`.
     - **Automatically trigger a new Request Phase** with the updated history so the model can process the tool output.

## 3. Tool Specifications

| Tool Name | Parameters | Implementation Details |
| :--- | :--- | :--- |
| `calculator` | `expression` (string) | Uses `eval()` or a safe math parser to evaluate mathematical expressions. Returns the result as a string. |
| `get_current_time` | None | Returns `new Date().toISOString()`. |
| `change_ui_color` | `color` (string: red, green, blue, default) | Updates a React state variable that controls a CSS class or inline style on the main chat container. |
| `show_notification`| `message` (string), `type` (string: info, success, warning, error) | Pushes a notification object to a React state array, rendering a temporary toast notification in the UI. Returns "Notification shown". |

## 4. UI/UX Changes
- **Tool Toggle:** A new toggle button `[ TOOLS ]` next to the existing `THINK` and `REASON` buttons. When disabled, the `tools` array is omitted from the API request.
- **Tool Call Visualization:** When a tool is invoked, render a distinct message block in the chat history (e.g., a grayed-out "system" style message showing the tool name, arguments, and the final result).
- **Toast Notifications:** A new overlay component to render alerts triggered by the `show_notification` tool.

## 5. Constraints & Error Handling
- **Max Iterations:** Implement a hard limit (e.g., `MAX_ITERATIONS = 5`) to prevent infinite agent loops if the model gets stuck calling tools repeatedly.
- **Error Boundaries:** Wrap tool execution in `try/catch` blocks. If a tool fails (e.g., invalid JSON from the model, or division by zero in the calculator), the error message is fed back to the model as the tool result, allowing the model to correct itself.
- **Provider Support:** Not all models support tools. The UI will attempt to pass tools if the toggle is active, and rely on standard API error handling if the provider rejects the payload.
