document.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll(".navbar").forEach(navbar => {
        const nav = navbar.querySelector("nav");
        if (!nav || navbar.querySelector(".nav-toggle")) return;

        const toggle = document.createElement("button");
        const quickActions = document.createElement("div");
        quickActions.className = "nav-quick-actions";

        // SVG Icons instead of emojis
        const icons = {
            cart: `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="21" r="1"></circle><circle cx="20" cy="21" r="1"></circle><path d="M1 1h4l2.68 13.39a2 2 0 0 0 2 1.61h9.72a2 2 0 0 0 2-1.61L23 6H6"></path></svg>`,
            notifications: `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"></path><path d="M13.73 21a2 2 0 0 1-3.46 0"></path></svg>`
        };

        [["/cart", "Cart", icons.cart], ["/notifications", "Notifications", icons.notifications]].forEach(([href, label, icon]) => {
            if (!nav.querySelector(`a[href="${href}"]`)) return;
            const link = document.createElement("a");
            link.href = href;
            link.setAttribute("aria-label", label);
            link.title = label;
            link.innerHTML = icon;
            quickActions.appendChild(link);
        });

        toggle.type = "button";
        toggle.className = "nav-toggle";
        toggle.setAttribute("aria-label", "Open navigation");
        toggle.setAttribute("aria-expanded", "false");
        toggle.innerHTML = `<span aria-hidden="true">☰</span>`;
        navbar.insertBefore(quickActions, nav);
        navbar.insertBefore(toggle, nav);

        toggle.addEventListener("click", () => {
            const open = navbar.classList.toggle("nav-open");
            toggle.setAttribute("aria-expanded", String(open));
            toggle.setAttribute("aria-label", open ? "Close navigation" : "Open navigation");
            toggle.innerHTML = `<span aria-hidden="true">${open ? "×" : "☰"}</span>`;
        });

        nav.addEventListener("click", event => {
            if (event.target.closest("a")) { 
                navbar.classList.remove("nav-open"); 
                toggle.setAttribute("aria-expanded", "false"); 
            }
        });
    });
});