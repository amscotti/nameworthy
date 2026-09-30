// Package character owns the WoW: Forever roster used by both the page and API.
// The matrix follows Blizzard's September 23, 2026 announcement:
// https://news.blizzard.com/en-us/article/24304075/create-the-hero-you-want-to-be-in-world-of-warcraft-forever
package character

import (
	"fmt"
	"slices"
	"strings"
)

const (
	Alliance = "Alliance"
	Horde    = "Horde"
	iconBase = "https://wow.zamimg.com/images/wow/icons/large/"
)

// Race is a playable option. Skyborne has two entries because faction changes
// its available classes. Name is the canonical value accepted by the API.
type Race struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Variant     string   `json:"variant,omitempty"`
	Faction     string   `json:"faction"`
	Icon        string   `json:"icon"`
	Description string   `json:"description"`
	Classes     []string `json:"classes"`
	NewClasses  []string `json:"new_classes"`
}

// Class is one of Forever's nine Classic-era classes.
type Class struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
}

var races = []Race{
	{"human", "Human", "Human", "", Alliance, iconBase + "race_human_male.jpg",
		"Versatile adventurers of Stormwind; family names, places, and earned titles.",
		[]string{"Warrior", "Paladin", "Hunter", "Rogue", "Priest", "Mage", "Warlock"}, []string{"Hunter"}},
	{"dwarf", "Dwarf", "Dwarf", "", Alliance, iconBase + "race_dwarf_male.jpg",
		"Khaz Modan's hardy explorers; clan names and stone, metal, or mountain imagery.",
		[]string{"Warrior", "Paladin", "Hunter", "Rogue", "Priest", "Shaman"}, []string{"Shaman"}},
	{"night-elf", "Night Elf", "Night Elf", "", Alliance, iconBase + "race_nightelf_male.jpg",
		"Ancient kaldorei of the forests; moon, stars, nature, and long ancestral histories.",
		[]string{"Warrior", "Hunter", "Rogue", "Priest", "Druid"}, nil},
	{"gnome", "Gnome", "Gnome", "", Alliance, iconBase + "race_gnome_male.jpg",
		"Gnomeregan's inventive exiles; playful names, clever wordplay, and mechanical surnames.",
		[]string{"Warrior", "Rogue", "Priest", "Mage", "Warlock"}, []string{"Priest"}},
	{"skyborne-alliance", "Skyborne (High Order)", "Skyborne", "High Order", Alliance,
		"https://wowclassicforever.info/images/races/skyborne.webp",
		"Shen'dorei descended from Eldre'Thalas scholars, reclaiming their arcane heritage. The Alliance's High Order can be Mages, but not Shaman.",
		[]string{"Warrior", "Hunter", "Rogue", "Mage", "Druid"}, []string{"Warrior", "Hunter", "Rogue", "Mage", "Druid"}},
	{"orc", "Orc", "Orc", "", Horde, iconBase + "race_orc_male.jpg",
		"Draenor-born warriors and mystics; clan ties, honor, and earned deeds.",
		[]string{"Warrior", "Hunter", "Rogue", "Shaman", "Mage", "Warlock"}, []string{"Mage"}},
	{"undead", "Undead", "Undead", "", Horde, iconBase + "race_scourge_male.jpg",
		"The Forsaken of Lordaeron; old human identities, dark irony, and reclaimed purpose.",
		[]string{"Warrior", "Paladin", "Rogue", "Priest", "Mage", "Warlock"}, []string{"Paladin"}},
	{"tauren", "Tauren", "Tauren", "", Horde, iconBase + "race_tauren_male.jpg",
		"People of the Earth Mother; tribal names, the plains, and reverence for nature.",
		[]string{"Warrior", "Hunter", "Shaman", "Druid"}, nil},
	{"troll", "Troll", "Troll", "", Horde, iconBase + "race_troll_male.jpg",
		"The Darkspear of Kalimdor; tribal heritage, the loa, and vivid personal epithets.",
		[]string{"Warrior", "Hunter", "Rogue", "Priest", "Shaman", "Mage", "Warlock"}, []string{"Warlock"}},
	{"skyborne-horde", "Skyborne (Windshaper)", "Skyborne", "Windshaper", Horde,
		"https://wowclassicforever.info/images/races/skyborne.webp",
		"Shen'dorei from Skywall, devoted to their missing wind-spirit mentors. The Horde's Windshapers can be Shaman, but not Mages.",
		[]string{"Warrior", "Hunter", "Rogue", "Shaman", "Druid"}, []string{"Warrior", "Hunter", "Rogue", "Shaman", "Druid"}},
}

var classes = []Class{
	{"warrior", "Warrior", "#C79C6E", iconBase + "classicon_warrior.jpg", "Arms & armor"},
	{"paladin", "Paladin", "#F58CBA", iconBase + "classicon_paladin.jpg", "Champion of the Light"},
	{"hunter", "Hunter", "#ABD473", iconBase + "classicon_hunter.jpg", "Tracker & beastmaster"},
	{"rogue", "Rogue", "#FFF569", iconBase + "classicon_rogue.jpg", "Stealth & precision"},
	{"priest", "Priest", "#FFFFFF", iconBase + "classicon_priest.jpg", "Light & shadow"},
	{"shaman", "Shaman", "#0070DE", iconBase + "classicon_shaman.jpg", "Voice of the elements"},
	{"mage", "Mage", "#69CCF0", iconBase + "classicon_mage.jpg", "Arcane, fire & frost"},
	{"warlock", "Warlock", "#9482C9", iconBase + "classicon_warlock.jpg", "Fel magic & demons"},
	{"druid", "Druid", "#FF7D0A", iconBase + "classicon_druid.jpg", "Shapeshifter & guardian"},
}

// Catalog is serialized into the page so JavaScript cannot drift from the API.
type Catalog struct {
	Races   []Race  `json:"races"`
	Classes []Class `json:"classes"`
}

// All returns independent slices, including the race's class lists.
func All() Catalog {
	r := slices.Clone(races)
	for i := range r {
		r[i].Classes = slices.Clone(r[i].Classes)
		r[i].NewClasses = slices.Clone(r[i].NewClasses)
	}
	return Catalog{Races: r, Classes: slices.Clone(classes)}
}

// ByRace finds a canonical race name, accepting case-insensitive API input.
func ByRace(name string) (Race, bool) {
	for _, r := range races {
		if strings.EqualFold(r.Name, name) {
			r.Classes = slices.Clone(r.Classes)
			r.NewClasses = slices.Clone(r.NewClasses)
			return r, true
		}
	}
	return Race{}, false
}

// ByClass finds a canonical class name.
func ByClass(name string) (Class, bool) {
	for _, c := range classes {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return Class{}, false
}

// Resolve validates optional context and returns canonical names. Invalid
// combinations are rejected before an audit can spend model tokens.
func Resolve(race, class string) (string, string, error) {
	var r Race
	if race != "" {
		var ok bool
		r, ok = ByRace(race)
		if !ok {
			if strings.EqualFold(race, "Skyborne") {
				return "", "", fmt.Errorf("choose Skyborne (High Order) or Skyborne (Windshaper) to specify a faction")
			}
			return "", "", fmt.Errorf("choose a playable WoW: Forever race")
		}
		race = r.Name
	}
	if class != "" {
		c, ok := ByClass(class)
		if !ok {
			return "", "", fmt.Errorf("choose one of WoW: Forever's nine classes")
		}
		class = c.Name
	}
	if race != "" && class != "" && !slices.Contains(r.Classes, class) {
		return "", "", fmt.Errorf("%s cannot be a %s in WoW: Forever — choose an available class", race, class)
	}
	return race, class, nil
}
