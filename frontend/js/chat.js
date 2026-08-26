(() => {
    const userRows = [...document.querySelectorAll(".person-row[data-user-id]")];
    const conversationCards = [...document.querySelectorAll(".conversation-card[data-user-id]")];

    if (userRows.length === 0 && conversationCards.length === 0) {
        return;
    }

    const updatePresence = async () => {
        try {
            const response = await fetch("/chat/presence", {
                headers: { Accept: "application/json" },
                cache: "no-store",
            });

            if (!response.ok) {
                return;
            }

            const data = await response.json();
            const onlineIDs = new Set((data.online_user_ids || []).map(String));

            userRows.forEach((row) => {
                const isOnline = onlineIDs.has(row.dataset.userId);
                const label = row.querySelector(".presence-status span");
                row.classList.toggle("is-online", isOnline);
                if (label) {
                    label.textContent = isOnline ? "Online" : "Offline";
                }
            });
            conversationCards.forEach((card) => {
                const isOnline = onlineIDs.has(card.dataset.userId);
                const label = card.querySelector(".conversation-presence span");
                card.classList.toggle("is-online", isOnline);
                if (label) label.textContent = isOnline ? "Online" : "Offline";
            });
            const conversations = document.querySelector(".conversation-list");
            if (conversations) [...conversations.children].sort((a,b) => Number(b.classList.contains("is-online")) - Number(a.classList.contains("is-online"))).forEach((card) => conversations.appendChild(card));
            const list = document.querySelector('.people-list');
            if (list) {
                [...list.children].sort((a, b) => Number(b.classList.contains('is-online')) - Number(a.classList.contains('is-online'))).forEach((row) => list.appendChild(row));
            }
        } catch {
            // Keep the last known state when the network is temporarily unavailable.
        }
    };

    updatePresence();
    window.setInterval(updatePresence, 30000);
    document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "visible") {
            updatePresence();
        }
    });
})();
