// Set before anything else. The mobile rules that hide the nav behind a toggle
// are scoped to html.js, so a browser with JavaScript disabled keeps a usable
// nav instead of losing it entirely.
document.documentElement.classList.add("js");

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

    // ==========================================================
    // BOTTOM TAB BAR
    //
    // A hamburger in the top corner is routinely missed by
    // first-time smartphone users, so the core destinations get
    // permanent tabs at the bottom of the screen. Everything is
    // built from the links the page already renders - no page
    // markup changes, and a tab can never point at a page this
    // section does not reach.
    // ==========================================================

    // Named tabIcons rather than icons so it cannot be confused with the icon
    // set the header toggle uses above.
    const tabIcons = {
        home: `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V21h14V9.5"/><path d="M9.5 21v-6h5v6"/></svg>`,
        market: `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7h18l-1.2 12.2a2 2 0 0 1-2 1.8H6.2a2 2 0 0 1-2-1.8Z"/><path d="M8.5 7V5.5a3.5 3.5 0 0 1 7 0V7"/></svg>`,
        wallet: `<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="6" width="18" height="13" rx="2.5"/><path d="M3 10.5h18"/></svg>`,
        chat: `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M21 12a8 8 0 0 1-8 8H4l2-3a8 8 0 1 1 15-5Z"/></svg>`,
        profile: `<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="8.5" r="3.5"/><path d="M4.5 20a7.5 7.5 0 0 1 15 0"/></svg>`,
        more: `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5.5 12h.01"/><path d="M12 12h.01"/><path d="M18.5 12h.01"/></svg>`
    };

    // Priority order. Whichever of these the page's own nav links to becomes a
    // tab; the rest stay in the More menu.
    const coreRoutes = [
        ["/dashboard", "Home", tabIcons.home],
        ["/marketplace", "Market", tabIcons.market],
        ["/wallet", "Wallet", tabIcons.wallet],
        ["/chat", "Chat", tabIcons.chat],
        ["/profile", "Profile", tabIcons.profile]
    ];

    // Four tabs plus More, so each keeps a comfortable tap target at 320px.
    const maxTabs = 4;

    document.querySelectorAll(".navbar").forEach(navbar => {
        const nav = navbar.querySelector("nav");
        if (!nav) return;

        const tabs = coreRoutes
            .filter(([href]) => nav.querySelector(`a[href="${href}"]`))
            .slice(0, maxTabs);

        // The landing page and the auth pages have a header but no app
        // navigation. They are left exactly as they are.
        if (tabs.length < 2) return;

        const bar = document.createElement("nav");
        bar.className = "mobile-tabbar";
        // Not "Main navigation": the header above is already a nav with that
        // label, and two identical landmarks are ambiguous to a screen reader.
        bar.setAttribute("aria-label", "Quick navigation");

        tabs.forEach(([href, label, icon]) => {
            const link = nav.querySelector(`a[href="${href}"]`);
            const tab = document.createElement("a");
            tab.href = href;
            // Reuse the marker the page already sets, so the tab bar agrees
            // with the top nav about which page you are on.
            if (link && link.getAttribute("aria-current") === "page") {
                tab.setAttribute("aria-current", "page");
            }
            tab.innerHTML = `${icon}<span>${label}</span>`;
            bar.appendChild(tab);
        });

        const more = document.createElement("button");
        more.type = "button";
        more.innerHTML = `${tabIcons.more}<span>More</span>`;
        more.setAttribute("aria-label", "More pages");

        // If the page you are on lives in the More menu rather than a tab,
        // mark More so the bar still shows where you are.
        const current = nav.querySelector('a[aria-current="page"]');
        if (current && !tabs.some(([href]) => current.getAttribute("href") === href)) {
            more.setAttribute("aria-current", "page");
        }

        more.addEventListener("click", () => {
            const open = navbar.classList.toggle("nav-open");
            const toggle = navbar.querySelector(".nav-toggle");
            if (toggle) {
                toggle.setAttribute("aria-expanded", String(open));
                toggle.setAttribute("aria-label", open ? "Close navigation" : "Open navigation");
                toggle.innerHTML = `<span aria-hidden="true">${open ? "×" : "☰"}</span>`;
            }
            // The menu hangs off the sticky header, so bring it into view.
            if (open) window.scrollTo({ top: 0, behavior: "smooth" });
        });

        bar.appendChild(more);
        document.body.appendChild(bar);
        // Drives the bottom padding in style.css. Set only where a bar was
        // really added, so pages without one keep their current spacing.
        document.body.classList.add("has-tabbar");
    });
});