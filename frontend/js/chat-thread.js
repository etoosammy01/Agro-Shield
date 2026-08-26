(() => {
    const form = document.querySelector('.message-form');
    const input = document.querySelector('.message-input');
    const conversation = form?.querySelector('[name="conversation_id"]')?.value;
    const status = document.getElementById('typing-status');
    if (!input || !conversation || !status) return;

    let typingTimer;
    let lastSent = false;
    const reportTyping = async (typing) => {
        if (typing === lastSent) return;
        lastSent = typing;
        try {
            await fetch('/chat/typing', {method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded'}, body: new URLSearchParams({conversation_id: conversation, typing: String(typing)})});
        } catch {}
    };
    input.addEventListener('input', () => {
        const typing = input.value.trim().length > 0;
        reportTyping(typing);
        clearTimeout(typingTimer);
        if (typing) typingTimer = setTimeout(() => reportTyping(false), 2500);
    });
    form.addEventListener('submit', () => reportTyping(false));
    window.addEventListener('beforeunload', () => reportTyping(false));

    const poll = async () => {
        try {
            const response = await fetch(`/chat/typing?conversation_id=${encodeURIComponent(conversation)}`, {cache: 'no-store'});
            if (response.ok) {
                const data = await response.json();
                status.textContent = data.typing ? `${data.name || 'Someone'} is typing…` : '';
            }
        } catch {}
    };
    poll();
    setInterval(poll, 1500);
})();
