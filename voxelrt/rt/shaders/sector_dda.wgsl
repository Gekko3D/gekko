// Shared object-space sector walk. Ray parameters stay in world-distance units:
// directions are neither normalized nor replaced by safe reciprocal clamps.
struct SectorDDA {
    position: vec3<i32>,
    t: f32,
    exit_t: f32,
    running: bool,
    origin: vec3<f32>,
    direction: vec3<f32>,
    step: vec3<i32>,
    lower: vec3<i32>,
    upper: vec3<i32>,
    next: vec3<f32>,
    end: f32,
    next_axis: u32,
}

fn sector_dda_finite(value: f32) -> bool {
    return (bitcast<u32>(value) & 0x7f800000u) != 0x7f800000u;
}

fn sector_dda_finite3(value: vec3<f32>) -> bool {
    return all((bitcast<vec3<u32>>(value) & vec3<u32>(0x7f800000u)) != vec3<u32>(0x7f800000u));
}

fn sector_dda_safe_coordinate(value: vec3<f32>) -> bool {
    // The upper bound is exclusive: f32(i32::MAX) rounds up to 2^31.
    return all(value >= vec3<f32>(-2147483648.0)) && all(value < vec3<f32>(2147483648.0));
}

fn sector_dda_boundary(origin: f32, direction: f32, coordinate: i32) -> f32 {
    let edge = (f32(coordinate) + select(0.0, 1.0, direction > 0.0)) * 32.0;
    return (edge - origin) / direction;
}

fn sector_dda_select_axis(direction: vec3<f32>, next: vec3<f32>) -> u32 {
    // Consider Z, Y, X in that order and replace only on a strict comparison.
    // Stationary axes never participate, including at zero-width end ties.
    var axis = 0u;
    if (direction.z != 0.0) { axis = 2u; }
    else if (direction.y != 0.0) { axis = 1u; }
    if (direction.y != 0.0 && next.y < next[axis]) { axis = 1u; }
    if (direction.x != 0.0 && next.x < next[axis]) { axis = 0u; }
    return axis;
}

fn sector_dda_refresh(state: ptr<function, SectorDDA>) {
    (*state).next_axis = sector_dda_select_axis((*state).direction, (*state).next);
    (*state).exit_t = max((*state).t, min((*state).next[(*state).next_axis], (*state).end));
}

fn sector_dda_init(origin: vec3<f32>, direction: vec3<f32>, bounds_min: vec3<f32>, bounds_max: vec3<f32>, t_start: f32, t_end: f32) -> SectorDDA {
    var state: SectorDDA;
    if (!sector_dda_finite3(origin) || !sector_dda_finite3(direction) ||
        !sector_dda_finite3(bounds_min) || !sector_dda_finite3(bounds_max) ||
        !sector_dda_finite(t_start) || !sector_dda_finite(t_end)) { return state; }
    if (any(bounds_min >= bounds_max) || t_start < 0.0 || t_start >= t_end ||
        all(direction == vec3<f32>(0.0))) { return state; }
    if (any((direction == vec3<f32>(0.0)) & ((origin < bounds_min) | (origin >= bounds_max)))) { return state; }

    let start_point = origin + direction * t_start;
    let end_point = origin + direction * t_end;
    if (!sector_dda_finite3(start_point) || !sector_dda_finite3(end_point)) { return state; }
    let object_lower = floor(bounds_min / 32.0);
    let object_upper = floor(bounds_max / 32.0);
    let start_floor = floor(start_point / 32.0);
    let end_floor = floor(end_point / 32.0);
    if (!sector_dda_safe_coordinate(object_lower) || !sector_dda_safe_coordinate(object_upper) ||
        !sector_dda_safe_coordinate(start_floor) || !sector_dda_safe_coordinate(end_floor)) { return state; }

    var start_cell = vec3<i32>(start_floor);
    // A negative ray starting exactly on a sector boundary enters its prior
    // cell. No epsilon moves non-boundary coordinates across their true edge.
    for (var axis = 0u; axis < 3u; axis++) {
        if (direction[axis] < 0.0 && start_point[axis] == start_floor[axis] * 32.0) {
            if (start_cell[axis] == (-2147483647 - 1)) { return state; }
            start_cell[axis] -= 1;
        }
    }
    let end_cell = vec3<i32>(end_floor);
    state.lower = max(vec3<i32>(object_lower), min(start_cell, end_cell));
    state.upper = min(vec3<i32>(object_upper), max(start_cell, end_cell));
    if (any(state.lower > state.upper)) { return state; }
    state.position = clamp(start_cell, state.lower, state.upper);
    state.origin = origin;
    state.direction = direction;
    state.step = vec3<i32>(sign(direction));
    state.t = t_start;
    state.end = t_end;
    state.running = true;
    for (var axis = 0u; axis < 3u; axis++) {
        if (direction[axis] != 0.0) {
            state.next[axis] = sector_dda_boundary(origin[axis], direction[axis], state.position[axis]);
        }
    }
    sector_dda_refresh(&state);
    return state;
}

fn sector_dda_advance(state: ptr<function, SectorDDA>) {
    if (!(*state).running) { return; }
    let axis = (*state).next_axis;
    // Integer terminal coordinates supply progress even when f32 crossing times
    // stagnate or round to the clip end. Check before adding to avoid overflow.
    if (((*state).step[axis] > 0 && (*state).position[axis] >= (*state).upper[axis]) ||
        ((*state).step[axis] < 0 && (*state).position[axis] <= (*state).lower[axis])) {
        (*state).running = false;
        return;
    }
    (*state).t = max((*state).t, min((*state).next[axis], (*state).end));
    (*state).position[axis] += (*state).step[axis];
    (*state).next[axis] = sector_dda_boundary((*state).origin[axis], (*state).direction[axis], (*state).position[axis]);
    sector_dda_refresh(state);
}
