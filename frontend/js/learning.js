(() => {
    const form = document.getElementById("learning-form");
    const input = document.getElementById("learning-question");
    const messageList = document.getElementById("chat-messages");
    const sendButton = document.getElementById("send-question");
    const clearButton = document.getElementById("clear-chat");
    const status = document.getElementById("learning-status");
    const farmingType = document.getElementById("farming-type");
    const customFarmingType = document.getElementById("custom-farming-type");
    const starters = document.querySelectorAll(".learning-starters [data-question]");

    if (!form || !input || !messageList || !sendButton || !clearButton || !status || !farmingType || !customFarmingType) return;

    let history = [];
    let historyCategory = "";
    let pending = false;
    const welcomeMessage = "Hello! What would you like to learn about farming today?";
    let typingMessage;

    function addMessage(content, role, isError = false) {
        const message = document.createElement("div");
        message.className = `learning-message learning-message-${role}${isError ? " learning-message-error" : ""}`;
        if (role === "assistant") {
            const label = document.createElement("span");
            label.className = "learning-message-label";
            label.textContent = "Agro-Shield learning guide";
            message.append(label);
        }
        const text = document.createElement("p");
        text.textContent = content;
        message.append(text);
        messageList.append(message);
        messageList.scrollTop = messageList.scrollHeight;
        return message;
    }

    function showTyping() {
        typingMessage = document.createElement("div");
        typingMessage.className = "learning-message learning-message-assistant learning-typing";
        typingMessage.setAttribute("aria-label", "Agro-Shield is preparing a response");
        const label = document.createElement("span");
        label.className = "learning-message-label";
        label.textContent = "Agro-Shield learning guide";
        const dots = document.createElement("span");
        dots.className = "learning-typing-dots";
        dots.setAttribute("aria-hidden", "true");
        for (let index = 0; index < 3; index += 1) dots.append(document.createElement("i"));
        const caption = document.createElement("span");
        caption.className = "learning-typing-caption";
        caption.textContent = "Putting together a helpful answer…";
        typingMessage.append(label, dots, caption);
        messageList.append(typingMessage);
        messageList.scrollTop = messageList.scrollHeight;
    }

    async function ask(question) {
        if (pending || !question) return;
        const category = farmingType.value === "other" ? customFarmingType.value.trim() : farmingType.value;
        if (!category) {
            status.textContent = farmingType.value === "other"
                ? "Enter the farming category you want to learn about."
                : "Choose a farming category before asking your question.";
            return;
        }
        if (historyCategory && historyCategory !== category) {
            history = [];
            messageList.replaceChildren();
            addMessage(welcomeMessage, "assistant");
        }
        pending = true;
        sendButton.disabled = true;
        input.disabled = true;
        farmingType.disabled = true;
        customFarmingType.disabled = true;
        status.textContent = "Agro-Shield AI is thinking...";
        addMessage(question, "user");
        showTyping();

        try {
            const messages = [...history.slice(-10), { role: "user", content: question }];
            const response = await fetch("/learning/chat", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ farmingType: category, messages })
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
            historyCategory = category;
            typingMessage?.remove();
            typingMessage = null;
            addMessage(result.answer, "assistant");
            status.textContent = "This chat is for learning. For urgent crop or animal health problems, contact a local extension worker or veterinarian.";
        } catch (error) {
            typingMessage?.remove();
            typingMessage = null;
            addMessage(error.message || "The assistant could not answer right now. Please try again.", "assistant", true);
            status.textContent = "Your question was not saved to the conversation. You can try again.";
        } finally {
            typingMessage?.remove();
            typingMessage = null;
            pending = false;
            sendButton.disabled = false;
            input.disabled = false;
            farmingType.disabled = false;
            customFarmingType.disabled = farmingType.value !== "other";
            input.focus();
        }
    }

    farmingType.addEventListener("change", () => {
        const isOther = farmingType.value === "other";
        customFarmingType.hidden = !isOther;
        customFarmingType.disabled = !isOther;
        customFarmingType.required = isOther;
        if (history.length > 0) {
            history = [];
            historyCategory = "";
            messageList.replaceChildren();
            addMessage(welcomeMessage, "assistant");
        }
        status.textContent = "Choose a farming category and ask a question about it.";
        if (isOther) customFarmingType.focus();
    });

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
        historyCategory = "";
        messageList.replaceChildren();
        addMessage(welcomeMessage, "assistant");
        status.textContent = "This chat is for learning. For urgent crop or animal health problems, contact a local extension worker or veterinarian.";
        input.focus();
    });

    starters.forEach((button) => {
        button.addEventListener("click", () => {
            const category = button.dataset.category || "";
            const categoryOption = [...farmingType.options].find((option) => option.value === category);
            if (categoryOption) {
                farmingType.value = category;
                farmingType.dispatchEvent(new Event("change"));
            }
            input.value = button.dataset.question || "";
            input.focus();
        });
    });
})();
