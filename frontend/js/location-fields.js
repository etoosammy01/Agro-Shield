(() => {
    const country = document.querySelector("[data-country-field]");
    const stateSelect = document.querySelector("[data-state-select]");
    const stateText = document.querySelector("[data-state-text]");
    const lgaSelect = document.querySelector("[data-lga-select]");
    const lgaText = document.querySelector("[data-lga-text]");
    const status = document.getElementById("location-directory-status");

    if (!country || !stateSelect || !stateText || !lgaSelect || !lgaText) return;

    const toggleField = (select, text, useSelect) => {
        select.hidden = !useSelect;
        select.style.display = useSelect ? "" : "none";
        select.disabled = !useSelect;
        select.required = useSelect;
        text.hidden = useSelect;
        text.style.display = useSelect ? "none" : "";
        text.disabled = useSelect;
        text.required = !useSelect;
    };

    const addOptions = (select, values, prompt, selectedValue = "") => {
        select.replaceChildren(new Option(prompt, ""));
        values.forEach((value) => select.add(new Option(value, value)));
        const selected = values.find((value) => value.toLowerCase() === selectedValue.trim().toLowerCase());
        if (selected) select.value = selected;
    };

    async function fetchOptions(path) {
        const response = await fetch(path, { headers: { Accept: "application/json" } });
        if (!response.ok) throw new Error("Location options could not be loaded.");
        const values = await response.json();
        if (!Array.isArray(values) || values.some((value) => typeof value !== "string")) {
            throw new Error("The location directory returned invalid options.");
        }
        return values;
    }

    async function loadLGAs(state, selectedLGA = "") {
        lgaSelect.disabled = true;
        addOptions(lgaSelect, [], "Loading local governments…");
        const query = new URLSearchParams({ state });
        const lgas = await fetchOptions(`/locations/nigeria/lgas?${query}`);
        addOptions(lgaSelect, lgas, "Choose your LGA", selectedLGA);
        lgaSelect.disabled = false;
    }

    async function useNigeriaDirectory() {
        toggleField(stateSelect, stateText, true);
        toggleField(lgaSelect, lgaText, true);
        stateSelect.disabled = true;
        lgaSelect.disabled = true;
        if (status) status.textContent = "Loading Nigerian states and local governments…";

        try {
            const selectedState = stateSelect.value || stateText.value || stateSelect.dataset.selected;
            const selectedLGA = lgaSelect.value || lgaText.value || lgaSelect.dataset.selected;
            const states = await fetchOptions("/locations/nigeria/states");
            addOptions(stateSelect, states, "Choose your state", selectedState);
            stateSelect.disabled = false;
            stateSelect.dataset.selected = stateSelect.value;
            if (stateSelect.value) {
                await loadLGAs(stateSelect.value, selectedLGA);
                lgaSelect.dataset.selected = lgaSelect.value;
            } else {
                addOptions(lgaSelect, [], "Choose a state first");
                lgaSelect.disabled = true;
            }
            if (status) status.textContent = "";
        } catch (error) {
            stateText.value = stateSelect.value || stateText.value || stateSelect.dataset.selected;
            lgaText.value = lgaSelect.value || lgaText.value || lgaSelect.dataset.selected;
            toggleField(stateSelect, stateText, false);
            toggleField(lgaSelect, lgaText, false);
            if (status) status.textContent = `${error.message} Enter your state and LGA manually, or try again later.`;
        }
    }

    function setForCountry() {
        if (country.value.trim().toLowerCase() === "nigeria") {
            useNigeriaDirectory();
        } else {
            stateText.value = stateSelect.value || stateText.value || stateSelect.dataset.selected;
            lgaText.value = lgaSelect.value || lgaText.value || lgaSelect.dataset.selected;
            toggleField(stateSelect, stateText, false);
            toggleField(lgaSelect, lgaText, false);
            stateSelect.replaceChildren(new Option("Select Nigeria as the country for state options", ""));
            lgaSelect.replaceChildren(new Option("Enter your LGA manually", ""));
            if (status) status.textContent = "Enter the state or region and district for this country.";
        }
    }
    country.addEventListener("change", setForCountry);
    stateSelect.addEventListener("change", async () => {
        stateSelect.dataset.selected = stateSelect.value;
        lgaSelect.dataset.selected = "";
        lgaText.value = "";
        if (!stateSelect.value) {
            addOptions(lgaSelect, [], "Choose a state first");
            lgaSelect.disabled = true;
            return;
        }
        try {
            await loadLGAs(stateSelect.value);
            lgaSelect.dataset.selected = lgaSelect.value;
            if (status) status.textContent = "";
        } catch (error) {
            lgaText.value = lgaSelect.dataset.selected || lgaSelect.value || lgaText.value;
            toggleField(lgaSelect, lgaText, false);
            if (status) status.textContent = `${error.message} Enter your LGA manually, or try again later.`;
        }
    });

    setForCountry();
})();
