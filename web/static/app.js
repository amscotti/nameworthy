// The server supplies the Forever roster. Alpine owns character selection;
// HTMX owns the request and replaces only the folio. No frontend build step.
document.addEventListener("alpine:init", () => {
  Alpine.data("auditor", () => ({
    first: "",
    last: "",
    race: "",
    cls: "",
    faction: "",
    busy: false,
    selectionNote: "",
    catalog: { races: [], classes: [] },
    init() {
      this.catalog = JSON.parse(this.$el.dataset.catalog);
    },
    get fullName() {
      return [this.first.trim(), this.last.trim()].filter(Boolean).join(" ");
    },
    get identity() {
      return [this.race, this.cls].filter(Boolean).join(" · ");
    },
    get seen() {
      return [this.fullName, this.identity].filter(Boolean).join(" — ");
    },
    get selectedRace() {
      return this.catalog.races.find(r => r.name === this.race) || null;
    },
    get selectedClass() {
      return this.catalog.classes.find(c => c.name === this.cls) || null;
    },
    get accent() {
      return this.selectedClass?.color || "#bfa164";
    },
    get raceHint() {
      return this.selectedRace?.description || "Choose a race for lore context, or leave it open.";
    },
    get classCount() {
      const count = this.selectedRace?.classes.length || this.catalog.classes.length;
      return `${count} of ${this.catalog.classes.length} available`;
    },
    get classHint() {
      if (!this.selectedRace) return "Pick a race to see its available classes. A class on its own works, too.";
      return `${this.selectedRace.name} can play ${this.selectedRace.classes.length} classes. Dimmed classes are unavailable in Forever.`;
    },
    classAllowed(name) {
      return !this.selectedRace || this.selectedRace.classes.includes(name);
    },
    isNewClass(name) {
      return (this.selectedRace?.new_classes || []).includes(name);
    },
    chooseRace(name) {
      if (this.busy) return;
      const next = this.catalog.races.find(r => r.name === name);
      this.selectionNote = "";
      if (this.cls && next && !next.classes.includes(this.cls)) {
        this.selectionNote = `${this.cls} is unavailable to ${next.name}. Class cleared; choose an available class.`;
        this.cls = "";
      }
      this.race = name;
    },
    chooseClass(name) {
      if (this.busy || (name && !this.classAllowed(name))) return;
      this.cls = name;
      this.selectionNote = "";
    },
    chooseFaction(name) {
      if (this.busy) return;
      this.selectionNote = "";
      if (this.selectedRace && name && this.selectedRace.faction !== name) {
        this.selectionNote = `${this.selectedRace.name} belongs to the ${this.selectedRace.faction}. Race cleared; choose a ${name} race.`;
        this.race = "";
      }
      this.faction = name;
    },
    clearCharacter() {
      if (this.busy) return;
      this.race = this.cls = this.faction = "";
      this.selectionNote = "Character context cleared. Your name will be audited on its own.";
    },
    useExample(key) {
      if (this.busy) return;
      const examples = {
        dwarf: ["Borin", "Stormforge", "Dwarf", "Shaman"],
        undead: ["Elara", "Ashenlight", "Undead", "Paladin"],
        skyborne: ["Mira", "Windweaver", "Skyborne (Windshaper)", "Shaman"],
      };
      const example = examples[key];
      if (!example) return;
      [this.first, this.last, this.race, this.cls] = example;
      this.faction = this.selectedRace.faction;
      this.selectionNote = "Example loaded. Edit it or audit this name.";
    },
  }));
});

(() => {
  const results = document.getElementById("results");
  const status = document.getElementById("audit-status");
  const connectionError = document.getElementById("connection-error");
  const isAudit = event => event.detail?.ctx?.target === results;

  function revealReading() {
    const heading = results.querySelector("#reading-title");
    if (!heading) return;
    heading.focus({ preventScroll: true });
    const bounds = heading.getBoundingClientRect();
    if (bounds.top < 24 || bounds.bottom > window.innerHeight - 24) {
      heading.closest(".reading").scrollIntoView({
        behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth",
        block: "start",
      });
    }
  }

  function setBusy(busy) {
    results.setAttribute("aria-busy", String(busy));
    document.dispatchEvent(new CustomEvent("audit-busy", { detail: busy }));
  }

  document.addEventListener("htmx:before:request", event => {
    if (!isAudit(event)) return;
    setBusy(true);
    status.textContent = "The ledger is auditing your name. Please wait.";
  });
  document.addEventListener("htmx:after:swap", event => {
    if (!isAudit(event)) return;
    status.textContent = "";
    requestAnimationFrame(revealReading);
  });
  document.addEventListener("htmx:finally:request", event => {
    if (isAudit(event)) setBusy(false);
  });
  document.addEventListener("htmx:error", event => {
    if (!isAudit(event) || event.detail.error?.name === "AbortError") return;
    results.replaceChildren(connectionError.content.cloneNode(true));
    status.textContent = "The ledger lost the thread. Check your connection and try again.";
    requestAnimationFrame(revealReading);
  });

  // Labels remain readable when a remote artwork host is unavailable. The
  // initial behind each image supplies a quiet fallback, with no broken icon.
  function imageFallback(image) {
    if (image.matches(".character-icon")) image.classList.add("asset-unavailable");
  }
  document.addEventListener("error", event => {
    if (event.target instanceof HTMLImageElement) imageFallback(event.target);
  }, true);
  document.addEventListener("load", event => {
    if (event.target instanceof HTMLImageElement && event.target.matches(".character-icon")) {
      event.target.classList.remove("asset-unavailable");
    }
  }, true);
  document.querySelectorAll("img.character-icon").forEach(image => {
    if (image.complete && image.naturalWidth === 0) imageFallback(image);
  });
})();
