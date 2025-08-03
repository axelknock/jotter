let debounceTimer;
let eventSource;
let lastWriter = "";
let debounce = 400;
let isConnected = false;
let heartbeatTimer;
let heartbeatTimeout = 10000; // 10 seconds (5s + 5s buffer)

function handleInput() {
  if (!isConnected) return;

  const content = document.getElementById("jot-field").value;
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => {
    fetch("/write", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ content: content }),
    });
  }, debounce);
}

function setConnectionState(connected) {
  isConnected = connected;
  const jotField = document.getElementById("jot-field");
  if (connected) {
    jotField.classList.remove("disconnected");
    jotField.placeholder = "Start typing...";
  } else {
    jotField.classList.add("disconnected");
    jotField.placeholder = "Disconnected - trying to reconnect...";
  }
}

function resetHeartbeatTimer() {
  clearTimeout(heartbeatTimer);
  heartbeatTimer = setTimeout(() => {
    console.log("Heartbeat timeout - connection lost");
    setConnectionState(false);
    if (eventSource) {
      eventSource.close();
    }
    setTimeout(connectSSE, 1000);
  }, heartbeatTimeout);
}

function connectSSE() {
  if (eventSource) {
    eventSource.close();
  }

  eventSource = new EventSource("/updates");

  eventSource.onopen = function () {
    console.log("SSE connection established");
    setConnectionState(true);
    resetHeartbeatTimer();
  };

  eventSource.onmessage = function (event) {
    // Reset heartbeat timer on any message
    resetHeartbeatTimer();

    // Process data messages
    if (event.data && event.data.trim() !== "") {
      try {
        const data = JSON.parse(event.data);
        if (data.type === "content_update" && data.writer !== getSessionId()) {
          const jotField = document.getElementById("jot-field");
          jotField.value = data.content;
        }
        // Heartbeat messages are handled by just resetting the timer above
      } catch (e) {
        console.log("Error parsing SSE message:", e);
      }
    }
  };

  eventSource.onerror = function () {
    console.log("SSE error occurred");
    clearTimeout(heartbeatTimer);
    // Don't immediately reconnect - let heartbeat timeout handle it
  };
}

function getSessionId() {
  let sessionId = sessionStorage.getItem("sessionId");
  if (!sessionId) {
    sessionId = Math.random().toString(36).substring(2, 15);
    sessionStorage.setItem("sessionId", sessionId);
  }
  return sessionId;
}

if (window.location.search.includes("token=")) {
  const url = new URL(window.location);
  url.searchParams.delete("token");
  url.searchParams.delete("new");
  window.history.replaceState({}, "", url.pathname + url.search);
}

// Initialize as disconnected until SSE connects
setConnectionState(false);
connectSSE();
