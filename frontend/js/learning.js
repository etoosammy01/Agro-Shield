(() => {
    const form = document.getElementById("learning-form");
    const input = document.getElementById("learning-question");
    const messageList = document.getElementById("chat-messages");
    const sendButton = document.getElementById("send-question");
    const clearButton = document.getElementById("clear-chat");
    const status = document.getElementById("learning-status");
    const starters = document.querySelectorAll(".learning-starters [data-question]");

    if (!form || !input || !messageList || !sendButton || !clearButton || !status) return;

    let history = [];
    let pending = false;
    const welcomeMessage = "Hello! What would you like to learn about farming today?";

    function addMessage(content, role, isError = false) {
        const message = document.createElement("div");
        message.className = `learning-message learning-message-${role}${isError ? " learning-message-error" : ""}`;
        const text = document.createElement("p");
        text.textContent = content;
        message.append(text);
        messageList.append(message);
        messageList.scrollTop = messageList.scrollHeight;
        return message;
    }

    async function ask(question) {
        if (pending || !question) return;
        pending = true;
        sendButton.disabled = true;
        input.disabled = true;
        status.textContent = "Agro-Shield AI is thinking...";
        addMessage(question, "user");

        try {
            const messages = [...history.slice(-10), { role: "user", content: question }];
            const response = await fetch("/learning/chat", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ messages })
            });
            if (!response.ok) {
                const detail = await response.text();
                throw new Error(detail.trim() || "The assistant could not answer right now.");
            }
            const result = await response.json();
            if (typeof result.answer !== "string" || !result.answer.trim()) {
                throw new Error("The assistant returned an empty answer. Please try again.");
            }

            history = [...messages, { role: "assistant", content: result.answer }].slice(-12);
            addMessage(result.answer, "assistant");
            status.textContent = "This chat is for learning. For urgent crop or animal health problems, contact a local extension worker or veterinarian.";
        } catch (error) {
            addMessage(error.message || "The assistant could not answer right now. Please try again.", "assistant", true);
            status.textContent = "Your question was not saved to the conversation. You can try again.";
        } finally {
            pending = false;
            sendButton.disabled = false;
            input.disabled = false;
            input.focus();
        }
    }

    form.addEventListener("submit", (event) => {
        event.preventDefault();
        const question = input.value.trim();
        if (!question) return;
        input.value = "";
        ask(question);
    });

    clearButton.addEventListener("click", () => {
        if (pending) return;
        history = [];
        messageList.replaceChildren();
        addMessage(welcomeMessage, "assistant");
        status.textContent = "This chat is for learning. For urgent crop or animal health problems, contact a local extension worker or veterinarian.";
        input.focus();
    });

    starters.forEach((button) => {
        button.addEventListener("click", () => {
            input.value = button.dataset.question || "";
            input.focus();
        });
    });
})();
