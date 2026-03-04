package ecs

type ArchetypeView interface {
	GetComponent(id uint32) (any, bool)
	EachEntity(func(EntityID, int) bool)
}

func Map1[A any](views []ArchetypeView, id1 uint32, optionals map[uint32]struct{}, m func(EntityID, *A) bool) {
	for _, arch := range views {
		var comps1 []A
		noA := false
		if data, ok := arch.GetComponent(id1); ok {
			comps1 = data.([]A)
		} else if _, ok := optionals[id1]; ok {
			noA = true
		} else {
			continue
		}

		stopped := false
		arch.EachEntity(func(entityID EntityID, row int) bool {
			var a *A
			if noA {
				a = nil
			} else {
				a = &comps1[row]
			}
			if !m(entityID, a) {
				stopped = true
				return false
			}
			return true
		})
		if stopped {
			return
		}
	}
}

func Map2[A, B any](views []ArchetypeView, id1, id2 uint32, optionals map[uint32]struct{}, m func(EntityID, *A, *B) bool) {
	for _, arch := range views {
		var comps1 []A
		noA := false
		if data, ok := arch.GetComponent(id1); ok {
			comps1 = data.([]A)
		} else if _, ok := optionals[id1]; ok {
			noA = true
		} else {
			continue
		}

		var comps2 []B
		noB := false
		if data, ok := arch.GetComponent(id2); ok {
			comps2 = data.([]B)
		} else if _, ok := optionals[id2]; ok {
			noB = true
		} else {
			continue
		}

		stopped := false
		arch.EachEntity(func(entityID EntityID, row int) bool {
			var a *A
			if noA {
				a = nil
			} else {
				a = &comps1[row]
			}

			var b *B
			if noB {
				b = nil
			} else {
				b = &comps2[row]
			}

			if !m(entityID, a, b) {
				stopped = true
				return false
			}
			return true
		})
		if stopped {
			return
		}
	}
}

func Map3[A, B, C any](views []ArchetypeView, id1, id2, id3 uint32, optionals map[uint32]struct{}, m func(EntityID, *A, *B, *C) bool) {
	for _, arch := range views {
		var comps1 []A
		noA := false
		if data, ok := arch.GetComponent(id1); ok {
			comps1 = data.([]A)
		} else if _, ok := optionals[id1]; ok {
			noA = true
		} else {
			continue
		}

		var comps2 []B
		noB := false
		if data, ok := arch.GetComponent(id2); ok {
			comps2 = data.([]B)
		} else if _, ok := optionals[id2]; ok {
			noB = true
		} else {
			continue
		}

		var comps3 []C
		noC := false
		if data, ok := arch.GetComponent(id3); ok {
			comps3 = data.([]C)
		} else if _, ok := optionals[id3]; ok {
			noC = true
		} else {
			continue
		}

		stopped := false
		arch.EachEntity(func(entityID EntityID, row int) bool {
			var a *A
			if noA {
				a = nil
			} else {
				a = &comps1[row]
			}

			var b *B
			if noB {
				b = nil
			} else {
				b = &comps2[row]
			}

			var c *C
			if noC {
				c = nil
			} else {
				c = &comps3[row]
			}

			if !m(entityID, a, b, c) {
				stopped = true
				return false
			}
			return true
		})
		if stopped {
			return
		}
	}
}

func Map4[A, B, C, D any](views []ArchetypeView, id1, id2, id3, id4 uint32, optionals map[uint32]struct{}, m func(EntityID, *A, *B, *C, *D) bool) {
	for _, arch := range views {
		var comps1 []A
		noA := false
		if data, ok := arch.GetComponent(id1); ok {
			comps1 = data.([]A)
		} else if _, ok := optionals[id1]; ok {
			noA = true
		} else {
			continue
		}

		var comps2 []B
		noB := false
		if data, ok := arch.GetComponent(id2); ok {
			comps2 = data.([]B)
		} else if _, ok := optionals[id2]; ok {
			noB = true
		} else {
			continue
		}

		var comps3 []C
		noC := false
		if data, ok := arch.GetComponent(id3); ok {
			comps3 = data.([]C)
		} else if _, ok := optionals[id3]; ok {
			noC = true
		} else {
			continue
		}

		var comps4 []D
		noD := false
		if data, ok := arch.GetComponent(id4); ok {
			comps4 = data.([]D)
		} else if _, ok := optionals[id4]; ok {
			noD = true
		} else {
			continue
		}

		stopped := false
		arch.EachEntity(func(entityID EntityID, row int) bool {
			var a *A
			if noA {
				a = nil
			} else {
				a = &comps1[row]
			}

			var b *B
			if noB {
				b = nil
			} else {
				b = &comps2[row]
			}

			var c *C
			if noC {
				c = nil
			} else {
				c = &comps3[row]
			}

			var d *D
			if noD {
				d = nil
			} else {
				d = &comps4[row]
			}

			if !m(entityID, a, b, c, d) {
				stopped = true
				return false
			}
			return true
		})
		if stopped {
			return
		}
	}
}

func Map5[A, B, C, D, E any](views []ArchetypeView, id1, id2, id3, id4, id5 uint32, optionals map[uint32]struct{}, m func(EntityID, *A, *B, *C, *D, *E) bool) {
	for _, arch := range views {
		var comps1 []A
		noA := false
		if data, ok := arch.GetComponent(id1); ok {
			comps1 = data.([]A)
		} else if _, ok := optionals[id1]; ok {
			noA = true
		} else {
			continue
		}

		var comps2 []B
		noB := false
		if data, ok := arch.GetComponent(id2); ok {
			comps2 = data.([]B)
		} else if _, ok := optionals[id2]; ok {
			noB = true
		} else {
			continue
		}

		var comps3 []C
		noC := false
		if data, ok := arch.GetComponent(id3); ok {
			comps3 = data.([]C)
		} else if _, ok := optionals[id3]; ok {
			noC = true
		} else {
			continue
		}

		var comps4 []D
		noD := false
		if data, ok := arch.GetComponent(id4); ok {
			comps4 = data.([]D)
		} else if _, ok := optionals[id4]; ok {
			noD = true
		} else {
			continue
		}

		var comps5 []E
		noE := false
		if data, ok := arch.GetComponent(id5); ok {
			comps5 = data.([]E)
		} else if _, ok := optionals[id5]; ok {
			noE = true
		} else {
			continue
		}

		stopped := false
		arch.EachEntity(func(entityID EntityID, row int) bool {
			var a *A
			if noA {
				a = nil
			} else {
				a = &comps1[row]
			}
			var b *B
			if noB {
				b = nil
			} else {
				b = &comps2[row]
			}
			var c *C
			if noC {
				c = nil
			} else {
				c = &comps3[row]
			}
			var d *D
			if noD {
				d = nil
			} else {
				d = &comps4[row]
			}
			var e *E
			if noE {
				e = nil
			} else {
				e = &comps5[row]
			}

			if !m(entityID, a, b, c, d, e) {
				stopped = true
				return false
			}
			return true
		})
		if stopped {
			return
		}
	}
}
