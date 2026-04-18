# Implementation Plan: Client-Side Agent Loop (Ballistics Tools)

**Goal:** Implement a client-side agent loop in `Test.js` to handle tool calls for testing LLM function calling capabilities.

**Architecture:** Refactor `initiateEngagement` to support streaming tool calls, executing them locally in the browser, and feeding results back to the model.

**Tech Stack:** React, JavaScript.

---

### Task 1: Setup State and Basic UI for Tools

**Files:**
- Modify: `frontend/src/pages/Test.js`

- [ ] **Step 1: Add new state variables**
Add state for `notifications`, `themeColor`, and `useTools` (boolean).

- [ ] **Step 2: Add Notification Component**
Render an overlay for notifications if `notifications` array has items.

- [ ] **Step 3: Apply Theme**
Apply `themeColor` to the main wrapper div or container.

- [ ] **Step 4: Add Tools Toggle**
Add a `TOOLS` button next to `THINK` and `REASON` buttons to toggle `useTools` state.

### Task 2: Define Tool Schemas

**Files:**
- Modify: `frontend/src/pages/Test.js`

- [ ] **Step 1: Define `availableTools` array**
Create an array of objects matching the OpenAI tool schema for:
  - `calculator`
  - `get_current_time`
  - `change_ui_color`
  - `show_notification`

### Task 3: Implement Tool Execution Logic

**Files:**
- Modify: `frontend/src/pages/Test.js`

- [ ] **Step 1: Create `executeToolCall` function**
Write a function that takes a tool call name and parsed arguments, executes the corresponding logic (eval for calc, Date for time, state updates for UI/notifications), and returns a string result. Wrap in try/catch to return error messages.

### Task 4: Refactor Agent Loop (`initiateEngagement`)

**Files:**
- Modify: `frontend/src/pages/Test.js`

- [ ] **Step 1: Loop Structure**
Wrap the fetch call inside a while loop with a maximum iteration count (e.g., 5).

- [ ] **Step 2: Attach Tools**
If `useTools` is true, append `tools: availableTools` to the request payload.

- [ ] **Step 3: Parse `tool_calls`**
Modify the SSE parser. If `delta.tool_calls` exists, accumulate the function name and arguments string.

- [ ] **Step 4: Handle end of stream**
After the stream ends, if `tool_calls` were accumulated, parse their arguments, execute them using `executeToolCall`, append the result to `messages` as `role: "tool"`, and continue the loop. Otherwise, break the loop.

### Task 5: Visualize Tool Calls in Chat

**Files:**
- Modify: `frontend/src/pages/Test.js`

- [ ] **Step 1: Render Tool Messages**
Update the message mapping to handle `role: "tool"` differently (e.g., smaller, grayed out, monospaced) to show the user what happened.
Also handle rendering of the assistant's `tool_calls` request if it exists in the message history, to show "Executing tool X...".
