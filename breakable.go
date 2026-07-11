package gekko

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/go-gl/mathgl/mgl32"
)

type BreakableComponent struct {
	Kind              string
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3
	Health            float32
	MaxHealth         float32
	Material          string
	SpawnObject       string
	SpawnFlags        int
	TargetName        string
	Target            string
	Delay             float32
	SourceTag         string
	Tags              []string
	Broken            bool
	DamageTaken       float32
}

func DamageBreakableEntity(cmd *Commands, eid EntityId, damage float32, activator EntityId) (handled bool, broken bool) {
	if cmd == nil {
		return false, false
	}
	breakable, ok := cmd.GetComponent(eid, reflect.TypeOf(BreakableComponent{})).(*BreakableComponent)
	if !ok || breakable == nil {
		return false, false
	}
	return true, damageBreakable(cmd, eid, breakable, damage, activator, false)
}

func triggerBreakable(cmd *Commands, eid EntityId, breakable *BreakableComponent, activator EntityId) bool {
	return damageBreakable(cmd, eid, breakable, maxf(breakable.Health, 1), activator, true)
}

func damageBreakable(cmd *Commands, eid EntityId, breakable *BreakableComponent, damage float32, activator EntityId, triggered bool) bool {
	if cmd == nil || breakable == nil || breakable.Broken {
		return false
	}
	if !triggered && breakable.SpawnFlags&1 != 0 {
		return false
	}
	if damage <= 0 {
		damage = 1
	}
	health := breakable.Health
	if health <= 0 {
		health = 1
	}
	breakable.DamageTaken += damage
	breakable.Health = health - damage
	if breakable.Health > 0 {
		return false
	}
	breakable.Broken = true
	breakable.Health = 0
	spawnBreakablePickup(cmd, eid, breakable)
	if breakable.Target != "" {
		QueueTargetEvent(cmd, breakable.Target, breakable.Delay, activator, breakable.SourceTag)
	}
	cmd.RemoveEntity(eid)
	return true
}

func spawnBreakablePickup(cmd *Commands, breakableEntity EntityId, breakable *BreakableComponent) {
	if cmd == nil || breakable == nil {
		return
	}
	pickup, ok := breakableSpawnObjectPickup(breakable.SpawnObject)
	if !ok {
		return
	}
	pos := breakable.BoundsCenter
	parent, hasParent := cmd.GetComponent(breakableEntity, reflect.TypeOf(Parent{})).(*Parent)
	comps := []any{
		&TransformComponent{Position: pos, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Position: pos, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&PickupComponent{
			Kind:      "hl1_pickup",
			Category:  pickup.Category,
			Item:      pickup.Item,
			Amount:    pickup.Amount,
			ClassName: pickup.ClassName,
			SourceTag: breakable.SourceTag,
			Tags: append(append([]string(nil), breakable.Tags...),
				"source:hl1",
				"classname:"+pickup.ClassName,
				"pickup:hl1",
				"pickup_category:"+pickup.Category,
				"pickup_item:"+pickup.Item,
				"spawned_from_breakable",
			),
		},
	}
	if hasParent && parent != nil {
		comps = append(comps, &Parent{Entity: parent.Entity})
	}
	cmd.AddEntity(comps...)
}

type breakableSpawnPickupDef struct {
	ClassName string
	Category  string
	Item      string
	Amount    int
}

func breakableSpawnObjectPickup(spawnObject string) (breakableSpawnPickupDef, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(spawnObject))
	if err != nil {
		return breakableSpawnPickupDef{}, false
	}
	switch value {
	case 1:
		return breakableSpawnPickupDef{ClassName: "item_battery", Category: "item", Item: "battery", Amount: 15}, true
	case 2:
		return breakableSpawnPickupDef{ClassName: "item_healthkit", Category: "item", Item: "healthkit", Amount: 15}, true
	case 3:
		return breakableSpawnPickupDef{ClassName: "weapon_9mmhandgun", Category: "weapon", Item: "9mmhandgun", Amount: 1}, true
	case 4:
		return breakableSpawnPickupDef{ClassName: "ammo_9mmclip", Category: "ammo", Item: "9mmclip", Amount: 17}, true
	case 5:
		return breakableSpawnPickupDef{ClassName: "weapon_9mmar", Category: "weapon", Item: "9mmar", Amount: 1}, true
	case 6:
		return breakableSpawnPickupDef{ClassName: "ammo_9mmar", Category: "ammo", Item: "9mmar", Amount: 50}, true
	case 7:
		return breakableSpawnPickupDef{ClassName: "ammo_argrenades", Category: "ammo", Item: "argrenades", Amount: 2}, true
	case 8:
		return breakableSpawnPickupDef{ClassName: "weapon_shotgun", Category: "weapon", Item: "shotgun", Amount: 1}, true
	case 9:
		return breakableSpawnPickupDef{ClassName: "ammo_buckshot", Category: "ammo", Item: "buckshot", Amount: 12}, true
	case 10:
		return breakableSpawnPickupDef{ClassName: "weapon_crossbow", Category: "weapon", Item: "crossbow", Amount: 1}, true
	case 11:
		return breakableSpawnPickupDef{ClassName: "ammo_crossbow", Category: "ammo", Item: "crossbow", Amount: 1}, true
	case 12:
		return breakableSpawnPickupDef{ClassName: "weapon_357", Category: "weapon", Item: "357", Amount: 1}, true
	case 13:
		return breakableSpawnPickupDef{ClassName: "ammo_357", Category: "ammo", Item: "357", Amount: 1}, true
	case 14:
		return breakableSpawnPickupDef{ClassName: "weapon_rpg", Category: "weapon", Item: "rpg", Amount: 1}, true
	case 15:
		return breakableSpawnPickupDef{ClassName: "ammo_rpgclip", Category: "ammo", Item: "rpgclip", Amount: 1}, true
	case 16:
		return breakableSpawnPickupDef{ClassName: "ammo_gaussclip", Category: "ammo", Item: "gaussclip", Amount: 1}, true
	case 17:
		return breakableSpawnPickupDef{ClassName: "weapon_handgrenade", Category: "weapon", Item: "handgrenade", Amount: 1}, true
	case 18:
		return breakableSpawnPickupDef{ClassName: "weapon_tripmine", Category: "weapon", Item: "tripmine", Amount: 1}, true
	case 19:
		return breakableSpawnPickupDef{ClassName: "weapon_satchel", Category: "weapon", Item: "satchel", Amount: 1}, true
	case 20:
		return breakableSpawnPickupDef{ClassName: "weapon_snark", Category: "weapon", Item: "snark", Amount: 1}, true
	case 21:
		return breakableSpawnPickupDef{ClassName: "weapon_hornetgun", Category: "weapon", Item: "hornetgun", Amount: 1}, true
	default:
		return breakableSpawnPickupDef{}, false
	}
}
