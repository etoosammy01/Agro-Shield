/* =========================================================
   AGRO-SHIELD DASHBOARD
   Complete dashboard JavaScript
========================================================= */

document.addEventListener("DOMContentLoaded", () => {
    Dashboard.init();
});


/* =========================================================
   DASHBOARD CONTROLLER
========================================================= */

const Dashboard = (() => {

    /* ---------------------------------------------------------
       Configuration
    --------------------------------------------------------- */

    const config = {
        counterDuration: 1200,
        toastDuration: 3500,
        timeUpdateInterval: 1000,
        animationDelay: 60
    };


    /* ---------------------------------------------------------
       State
    --------------------------------------------------------- */

    let clockInterval = null;


    /* ---------------------------------------------------------
       DOM Helpers
    --------------------------------------------------------- */

    const $ = (selector, parent = document) => {
        return parent.querySelector(selector);
    };


    const $$ = (selector, parent = document) => {
        return Array.from(parent.querySelectorAll(selector));
    };


    /* ---------------------------------------------------------
       Reduced Motion
    --------------------------------------------------------- */

    const prefersReducedMotion = () => {
        return window.matchMedia(
            "(prefers-reduced-motion: reduce)"
        ).matches;
    };


    /* =========================================================
       INITIALIZATION
    ========================================================== */

    function init() {

        updateDate();
        startClock();
        updateGreeting();

        animateCounters();
        animateProgressBars();
        animateDashboardEntrance();

        setupCardInteractions();
        setupActionInteractions();
        setupNotificationInteractions();

        setupVisibilityHandling();
        setupReducedMotionListener();

    }


    /* =========================================================
       DATE
    ========================================================== */

    function updateDate() {

        const todayElement = $("#today");

        if (!todayElement) {
            return;
        }

        const now = new Date();

        const formatter = new Intl.DateTimeFormat(
            "en-NG",
            {
                weekday: "long",
                month: "long",
                day: "numeric",
                year: "numeric"
            }
        );

        todayElement.textContent =
            formatter.format(now).toUpperCase();

    }


    /* =========================================================
       LIVE CLOCK
    ========================================================== */

    function startClock() {

        updateClock();

        if (clockInterval) {
            clearInterval(clockInterval);
        }

        clockInterval = setInterval(
            updateClock,
            config.timeUpdateInterval
        );

    }


    function updateClock() {

        const timeElement = $("#currentTime");

        if (!timeElement) {
            return;
        }

        const now = new Date();

        const formatter = new Intl.DateTimeFormat(
            "en-NG",
            {
                hour: "numeric",
                minute: "2-digit",
                second: "2-digit",
                hour12: true
            }
        );

        timeElement.textContent =
            formatter.format(now);

    }


    /* =========================================================
       TIME-BASED GREETING
    ========================================================== */

    function updateGreeting() {

        const greetingElement = $("#greeting");

        if (!greetingElement) {
            return;
        }

        const hour = new Date().getHours();

        let greeting = "Welcome back";

        if (hour >= 5 && hour < 12) {
            greeting = "Good morning";
        } else if (hour >= 12 && hour < 17) {
            greeting = "Good afternoon";
        } else if (hour >= 17 && hour < 22) {
            greeting = "Good evening";
        } else {
            greeting = "Good night";
        }

        greetingElement.textContent = greeting;

    }


    /* =========================================================
       ANIMATED METRIC COUNTERS
    ========================================================== */

    function animateCounters() {

        const counters = $$(".counter");

        if (!counters.length) {
            return;
        }

        counters.forEach(counter => {

            const target = parseFloat(
                counter.dataset.target
            );

            if (Number.isNaN(target)) {
                return;
            }

            if (prefersReducedMotion()) {
                counter.textContent =
                    formatNumber(target);

                return;
            }

            animateNumber(counter, target);

        });

    }


    function animateNumber(
        element,
        target
    ) {

        const duration = config.counterDuration;

        const startTime = performance.now();

        function update(currentTime) {

            const elapsed =
                currentTime - startTime;

            const progress =
                Math.min(elapsed / duration, 1);

            /*
             * Ease-out curve.
             * Starts quickly and slows down naturally.
             */
            const eased =
                1 - Math.pow(1 - progress, 3);

            const current =
                target * eased;

            element.textContent =
                formatNumber(current);

            if (progress < 1) {
                requestAnimationFrame(update);
            } else {
                element.textContent =
                    formatNumber(target);
            }

        }

        requestAnimationFrame(update);

    }


    function formatNumber(value) {

        return new Intl.NumberFormat(
            "en-NG",
            {
                maximumFractionDigits: 0
            }
        ).format(value);

    }


    /* =========================================================
       PROGRESS BAR ANIMATION
    ========================================================== */

    function animateProgressBars() {

        const progressBars =
            $$(".progress-value");

        if (!progressBars.length) {
            return;
        }

        progressBars.forEach(bar => {

            const width =
                bar.dataset.width;

            if (!width) {
                return;
            }

            if (prefersReducedMotion()) {
                bar.style.width = width;
                return;
            }

            bar.style.width = "0%";

            requestAnimationFrame(() => {

                requestAnimationFrame(() => {

                    bar.style.width =
                        width;

                });

            });

        });

    }


    /* =========================================================
       DASHBOARD ENTRANCE ANIMATION
    ========================================================== */

    function animateDashboardEntrance() {

        if (prefersReducedMotion()) {
            return;
        }

        const sections = $$(
            ".dashboard > section, .dashboard > .dashboard-grid"
        );

        sections.forEach((section, index) => {

            section.style.opacity = "0";
            section.style.transform =
                "translateY(10px)";

            setTimeout(() => {

                section.style.transition =
                    "opacity 450ms ease, transform 450ms ease";

                section.style.opacity = "1";
                section.style.transform =
                    "translateY(0)";

            }, index * config.animationDelay);

        });

    }


    /* =========================================================
       CARD INTERACTIONS
    ========================================================== */

    function setupCardInteractions() {

        const cards = $$(
            ".metric-card, .action-card, .panel"
        );

        cards.forEach(card => {

            card.addEventListener(
                "mouseenter",
                () => {

                    if (prefersReducedMotion()) {
                        return;
                    }

                    card.style.setProperty(
                        "--interaction-scale",
                        "1"
                    );

                }
            );

        });

    }


    /* =========================================================
       ACTION INTERACTIONS
    ========================================================== */

    function setupActionInteractions() {

        const actionCards =
            $$(".action-card");

        actionCards.forEach(card => {

            card.addEventListener(
                "click",
                () => {

                    /*
                     * Don't interrupt normal navigation.
                     * This simply gives immediate visual feedback.
                     */
                    card.classList.add(
                        "action-clicked"
                    );

                    setTimeout(() => {

                        card.classList.remove(
                            "action-clicked"
                        );

                    }, 250);

                }
            );

        });


        const primaryActions =
            $$(".primary-action");

        primaryActions.forEach(button => {

            button.addEventListener(
                "mousedown",
                () => {

                    button.classList.add(
                        "button-pressed"
                    );

                }
            );

            button.addEventListener(
                "mouseup",
                () => {

                    button.classList.remove(
                        "button-pressed"
                    );

                }
            );

            button.addEventListener(
                "mouseleave",
                () => {

                    button.classList.remove(
                        "button-pressed"
                    );

                }
            );

        });

    }


    /* =========================================================
       NOTIFICATIONS
    ========================================================== */

    function setupNotificationInteractions() {

        const notifications =
            $$(".notification-row");

        notifications.forEach(notification => {

            notification.addEventListener(
                "click",
                () => {

                    notification.classList.add(
                        "notification-opened"
                    );

                }
            );

        });

    }


    /* =========================================================
       TOAST SYSTEM
    ========================================================== */

    function showToast(
        message,
        type = "success"
    ) {

        let container =
            $(".toast-container");

        if (!container) {

            container =
                document.createElement("div");

            container.className =
                "toast-container";

            document.body.appendChild(
                container
            );

        }


        const toast =
            document.createElement("div");

        toast.className =
            `toast toast-${type}`;

        toast.setAttribute(
            "role",
            "status"
        );


        const icon =
            document.createElement("span");

        icon.className =
            "toast-icon";

        icon.textContent =
            getToastIcon(type);


        const text =
            document.createElement("span");

        text.className =
            "toast-message";

        text.textContent =
            message;


        toast.appendChild(icon);
        toast.appendChild(text);

        container.appendChild(toast);


        requestAnimationFrame(() => {

            toast.classList.add(
                "toast-visible"
            );

        });


        setTimeout(() => {

            toast.classList.remove(
                "toast-visible"
            );

            setTimeout(() => {

                toast.remove();

            }, 250);

        }, config.toastDuration);

    }


    function getToastIcon(type) {

        const icons = {
            success: "✓",
            warning: "!",
            error: "×",
            info: "i"
        };

        return icons[type] || icons.info;

    }


    /* =========================================================
       VISIBILITY HANDLING
       Stops unnecessary clock work when tab is hidden.
    ========================================================== */

    function setupVisibilityHandling() {

        document.addEventListener(
            "visibilitychange",
            () => {

                if (document.hidden) {

                    if (clockInterval) {

                        clearInterval(
                            clockInterval
                        );

                        clockInterval = null;

                    }

                } else {

                    updateClock();
                    updateGreeting();

                    startClock();

                }

            }
        );

    }


    /* =========================================================
       REDUCED MOTION CHANGE LISTENER
    ========================================================== */

    function setupReducedMotionListener() {

        const mediaQuery =
            window.matchMedia(
                "(prefers-reduced-motion: reduce)"
            );

        mediaQuery.addEventListener(
            "change",
            () => {

                if (mediaQuery.matches) {
                    document.documentElement
                        .classList.add(
                            "reduced-motion"
                        );
                } else {
                    document.documentElement
                        .classList.remove(
                            "reduced-motion"
                        );
                }

            }
        );

    }


    /* =========================================================
       PUBLIC API
    ========================================================== */

    return {

        init,

        showToast,

        updateDate,

        updateClock,

        updateGreeting,

        animateCounters,

        animateProgressBars

    };

})();

/* ============================================================
   NEW FEATURES ADDED - Real-Time Activity Simulation
   ============================================================ */

(function() {
    const activityList = document.querySelector(".activity-list");
    if (!activityList) return;

    // Sample activities
    const sampleActivities = [
        { icon: "🌾", title: "New Product Listed", desc: "You listed 10kg of Fresh Tomatoes", value: "+₦5,000", time: "Just now" },
        { icon: "💰", title: "New Sale", desc: "Mary's Restaurant bought 5kg of Cassava", value: "+₦3,500", time: "Just now" },
        { icon: "📈", title: "Performance Update", desc: "Your farm revenue increased by 5%", value: "+5%", time: "2 mins ago" },
        { icon: "🌾", title: "New Product Listed", desc: "You listed 20kg of Organic Maize", value: "+₦8,000", time: "3 mins ago" },
        { icon: "💰", title: "New Sale", desc: "John's Supermarket bought 15kg of Yams", value: "+₦6,500", time: "5 mins ago" }
    ];

    function addActivity(activity) {
        const row = document.createElement("div");
        row.className = "activity-row";
        row.innerHTML = `
            <span class="activity-mark"></span>
            <div class="activity-main">
                <h3>${activity.icon} ${activity.title}</h3>
                <p>${activity.desc} • ${activity.time}</p>
            </div>
            <div class="activity-value">
                <strong>${activity.value}</strong>
                <span>${activity.time}</span>
            </div>
        `;
        activityList.insertBefore(row, activityList.firstChild);
    }

    // Add initial activities
    sampleActivities.forEach(activity => addActivity(activity));

    // Simulate real-time updates
    setInterval(() => {
        const newActivity = {
            icon: ["🌾", "💰", "📈"][Math.floor(Math.random() * 3)],
            title: ["New Product Listed", "New Sale", "Performance Update"][Math.floor(Math.random() * 3)],
            desc: ["You listed fresh vegetables", "A buyer purchased 10kg of produce", "Your farm rating improved"][Math.floor(Math.random() * 3)],
            value: ["+₦2,000", "+₦4,500", "+3%"][Math.floor(Math.random() * 3)],
            time: "Just now"
        };
        addActivity(newActivity);

        // Keep activity list short (max 10 items)
        while (activityList.children.length > 10) {
            activityList.removeChild(activityList.lastChild);
        }
    }, 10000); // Adds a new activity every 10 seconds
})();