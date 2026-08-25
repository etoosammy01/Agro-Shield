(() => {
    const userRows = [...document.querySelectorAll(".person-row[data-user-id]")];

    if (userRows.length === 0) {
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
