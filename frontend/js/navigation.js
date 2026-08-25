document.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll(".navbar").forEach(navbar => {
        const nav = navbar.querySelector("nav");
        if (!nav || navbar.querySelector(".nav-toggle")) return;
        const toggle = document.createElement("button");
        const quickActions = document.createElement("div");
        quickActions.className = "nav-quick-actions";
        [["/cart", "Cart", "🛒"], ["/notifications", "Notifications", "🔔"]].forEach(([href, label, icon]) => {
            if (!nav.querySelector(`a[href="${href}"]`)) return;
            const link = document.createElement("a");
            link.href = href;
            link.setAttribute("aria-label", label);
            link.title = label;
            link.textContent = icon;
            quickActions.appendChild(link);
        });
        toggle.type = "button";
        toggle.className = "nav-toggle";
        toggle.setAttribute("aria-label", "Open navigation");
        toggle.setAttribute("aria-expanded", "false");
        toggle.innerHTML = "<span aria-hidden=\"true\">☰</span>";
        navbar.insertBefore(quickActions, nav);
        navbar.insertBefore(toggle, nav);
        toggle.addEventListener("click", () => {
            const open = navbar.classList.toggle("nav-open");
            toggle.setAttribute("aria-expanded", String(open));
            toggle.setAttribute("aria-label", open ? "Close navigation" : "Open navigation");
            toggle.innerHTML = `<span aria-hidden="true">${open ? "×" : "☰"}</span>`;
        });
        nav.addEventListener("click", event => {
            if (event.target.closest("a")) { navbar.classList.remove("nav-open"); toggle.setAttribute("aria-expanded", "false"); }
        });
    });
});
