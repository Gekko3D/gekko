package content

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"
)

const (
	navBinaryVersion = uint16(1)
	navEndianMarker  = uint32(0x01020304)
	maxNavPayload    = uint64(2 << 30)
	maxNavCount      = uint32(100_000_000)
)

var (
	navSourceMagic = [8]byte{'G', 'K', 'N', 'S', 'T', 'I', 'L', 'E'}
	navGraphMagic  = [8]byte{'G', 'K', 'N', 'G', 'T', 'I', 'L', 'E'}
)

type navBinaryWriter struct {
	bytes.Buffer
	err error
}

func (w *navBinaryWriter) raw(v any) {
	if w.err == nil {
		w.err = binary.Write(&w.Buffer, binary.LittleEndian, v)
	}
}
func (w *navBinaryWriter) u8(v byte)                    { w.raw(v) }
func (w *navBinaryWriter) u32(v uint32)                 { w.raw(v) }
func (w *navBinaryWriter) u64(v uint64)                 { w.raw(v) }
func (w *navBinaryWriter) f32(v float32)                { w.u32(math.Float32bits(v)) }
func (w *navBinaryWriter) coord(v TerrainChunkCoordDef) { w.i(v.X); w.i(v.Y); w.i(v.Z) }
func (w *navBinaryWriter) vec3(v Vec3)                  { w.f32(v[0]); w.f32(v[1]); w.f32(v[2]) }
func (w *navBinaryWriter) i(v int) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		w.err = fmt.Errorf("navigation integer %d exceeds binary contract", v)
		return
	}
	w.raw(int32(v))
}
func (w *navBinaryWriter) count(v int) {
	if v < 0 || uint64(v) > uint64(maxNavCount) {
		w.err = fmt.Errorf("navigation count %d exceeds binary contract", v)
		return
	}
	w.u32(uint32(v))
}
func (w *navBinaryWriter) text(v string) {
	w.count(len(v))
	if w.err == nil {
		_, w.err = w.WriteString(v)
	}
}

type navBinaryReader struct {
	r   *bytes.Reader
	err error
}

func newNavBinaryReader(data []byte) *navBinaryReader {
	return &navBinaryReader{r: bytes.NewReader(data)}
}
func (r *navBinaryReader) raw(v any) {
	if r.err == nil {
		r.err = binary.Read(r.r, binary.LittleEndian, v)
	}
}
func (r *navBinaryReader) u8() byte     { var v byte; r.raw(&v); return v }
func (r *navBinaryReader) u32() uint32  { var v uint32; r.raw(&v); return v }
func (r *navBinaryReader) u64() uint64  { var v uint64; r.raw(&v); return v }
func (r *navBinaryReader) i() int       { var v int32; r.raw(&v); return int(v) }
func (r *navBinaryReader) f32() float32 { return math.Float32frombits(r.u32()) }
func (r *navBinaryReader) coord() TerrainChunkCoordDef {
	return TerrainChunkCoordDef{X: r.i(), Y: r.i(), Z: r.i()}
}
func (r *navBinaryReader) vec3() Vec3 { return Vec3{r.f32(), r.f32(), r.f32()} }
func (r *navBinaryReader) count() int {
	v := r.u32()
	if v > maxNavCount && r.err == nil {
		r.err = fmt.Errorf("navigation count %d exceeds binary contract", v)
	}
	return int(v)
}
func (r *navBinaryReader) text() string {
	n := r.count()
	if r.err != nil {
		return ""
	}
	data := make([]byte, n)
	_, r.err = io.ReadFull(r.r, data)
	return string(data)
}
func (r *navBinaryReader) done() error {
	if r.err != nil {
		return r.err
	}
	if r.r.Len() != 0 {
		return fmt.Errorf("navigation binary payload has %d trailing bytes", r.r.Len())
	}
	return nil
}

type navStrings struct {
	values []string
	index  map[string]uint32
}

func makeNavStrings(values map[string]struct{}) navStrings {
	list := make([]string, 0, len(values))
	for value := range values {
		list = append(list, value)
	}
	sort.Strings(list)
	index := make(map[string]uint32, len(list))
	for i, value := range list {
		index[value] = uint32(i)
	}
	return navStrings{values: list, index: index}
}
func (s navStrings) write(w *navBinaryWriter) {
	w.count(len(s.values))
	for _, value := range s.values {
		w.text(value)
	}
}
func readNavStrings(r *navBinaryReader) []string {
	count := r.count()
	if count == 0 {
		return nil
	}
	values := make([]string, count)
	for i := range values {
		values[i] = r.text()
	}
	return values
}
func (s navStrings) ref(w *navBinaryWriter, value string) { w.u32(s.index[value]) }
func navStringRef(r *navBinaryReader, values []string) string {
	index := r.u32()
	if int(index) >= len(values) {
		if r.err == nil {
			r.err = fmt.Errorf("navigation string reference %d is out of range", index)
		}
		return ""
	}
	return values[index]
}

func wrapNavBinary(magic [8]byte, schema int, payload []byte) ([]byte, error) {
	if uint64(len(payload)) > maxNavPayload {
		return nil, fmt.Errorf("navigation payload is too large")
	}
	var out bytes.Buffer
	out.Write(magic[:])
	_ = binary.Write(&out, binary.LittleEndian, navBinaryVersion)
	_ = binary.Write(&out, binary.LittleEndian, navEndianMarker)
	_ = binary.Write(&out, binary.LittleEndian, uint32(schema))
	_ = binary.Write(&out, binary.LittleEndian, uint64(len(payload)))
	zipper, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err = zipper.Write(payload); err == nil {
		err = zipper.Close()
	} else {
		_ = zipper.Close()
	}
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func unwrapNavBinary(data []byte, magic [8]byte, schema int) ([]byte, error) {
	const headerSize = 8 + 2 + 4 + 4 + 8
	if len(data) < headerSize || !bytes.Equal(data[:8], magic[:]) {
		return nil, fmt.Errorf("invalid navigation binary magic")
	}
	version := binary.LittleEndian.Uint16(data[8:10])
	endian := binary.LittleEndian.Uint32(data[10:14])
	gotSchema := binary.LittleEndian.Uint32(data[14:18])
	length := binary.LittleEndian.Uint64(data[18:26])
	if version != navBinaryVersion {
		return nil, fmt.Errorf("unsupported navigation binary version %d", version)
	}
	if endian != navEndianMarker {
		return nil, fmt.Errorf("unsupported navigation byte order")
	}
	if gotSchema != uint32(schema) {
		return nil, fmt.Errorf("unsupported navigation schema version %d", gotSchema)
	}
	if length > maxNavPayload {
		return nil, fmt.Errorf("navigation payload length %d exceeds limit", length)
	}
	zipper, err := gzip.NewReader(bytes.NewReader(data[headerSize:]))
	if err != nil {
		return nil, fmt.Errorf("open navigation gzip payload: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(zipper, int64(length)+1))
	closeErr := zipper.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read navigation gzip payload: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close navigation gzip payload: %w", closeErr)
	}
	if uint64(len(payload)) != length {
		return nil, fmt.Errorf("navigation payload length mismatch: got %d want %d", len(payload), length)
	}
	return payload, nil
}

func encodeNavSourceTile(def *NavSourceTileDef) ([]byte, error) {
	if !finite(def.VoxelResolution) || def.VoxelResolution <= 0 {
		return nil, fmt.Errorf("navigation source tile voxel_resolution must be finite and positive")
	}
	stringsSet := map[string]struct{}{def.NavID: {}, def.BuilderVersion: {}, def.SourceHash: {}, def.DependencyHash: {}}
	for _, span := range def.Spans {
		stringsSet[span.Area] = struct{}{}
		for _, flag := range span.Flags {
			stringsSet[flag] = struct{}{}
		}
	}
	strings := makeNavStrings(stringsSet)
	w := &navBinaryWriter{}
	strings.write(w)
	strings.ref(w, def.NavID)
	strings.ref(w, def.BuilderVersion)
	strings.ref(w, def.SourceHash)
	strings.ref(w, def.DependencyHash)
	w.coord(def.Coord)
	w.i(def.ChunkSize)
	w.f32(def.VoxelResolution)
	writeNavVoxelRuns(w, def.SolidRuns)
	writeNavVoxelRuns(w, def.BlockedRuns)
	w.count(len(def.Spans))
	for _, span := range def.Spans {
		w.i(span.X)
		w.i(span.Y)
		w.i(span.Z)
		w.i(int(math.Round(float64(span.SupportHeight / def.VoxelResolution))))
		w.i(int(math.Round(float64(span.CeilingHeight / def.VoxelResolution))))
		w.f32(span.ClearanceRadius / def.VoxelResolution)
		strings.ref(w, span.Area)
		w.count(len(span.Flags))
		for _, flag := range span.Flags {
			strings.ref(w, flag)
		}
	}
	if w.err != nil {
		return nil, w.err
	}
	return wrapNavBinary(navSourceMagic, def.SchemaVersion, w.Bytes())
}

func decodeNavSourceTile(data []byte) (*NavSourceTileDef, error) {
	payload, err := unwrapNavBinary(data, navSourceMagic, CurrentNavSourceTileSchemaVersion)
	if err != nil {
		return nil, err
	}
	r := newNavBinaryReader(payload)
	strings := readNavStrings(r)
	def := &NavSourceTileDef{SchemaVersion: CurrentNavSourceTileSchemaVersion}
	def.NavID = navStringRef(r, strings)
	def.BuilderVersion = navStringRef(r, strings)
	def.SourceHash = navStringRef(r, strings)
	def.DependencyHash = navStringRef(r, strings)
	def.Coord = r.coord()
	def.ChunkSize = r.i()
	def.VoxelResolution = r.f32()
	def.SolidRuns = readNavVoxelRuns(r)
	def.BlockedRuns = readNavVoxelRuns(r)
	if count := r.count(); count != 0 {
		def.Spans = make([]NavSpanDef, count)
	}
	for i := range def.Spans {
		span := &def.Spans[i]
		span.ID = uint32(i)
		span.X = r.i()
		span.Y = r.i()
		span.Z = r.i()
		span.SupportHeight = float32(r.i()) * def.VoxelResolution
		span.CeilingHeight = float32(r.i()) * def.VoxelResolution
		span.Headroom = span.CeilingHeight - span.SupportHeight
		span.ClearanceRadius = r.f32() * def.VoxelResolution
		span.Area = navStringRef(r, strings)
		if count := r.count(); count != 0 {
			span.Flags = make([]string, count)
		}
		for j := range span.Flags {
			span.Flags[j] = navStringRef(r, strings)
		}
	}
	if err := r.done(); err != nil {
		return nil, fmt.Errorf("decode navigation source tile: %w", err)
	}
	return def, nil
}

func writeNavVoxelRuns(w *navBinaryWriter, runs []NavVoxelRunDef) {
	w.count(len(runs))
	for _, run := range runs {
		w.i(run.X)
		w.i(run.Y)
		w.i(run.Z)
		w.i(run.Count)
	}
}
func readNavVoxelRuns(r *navBinaryReader) []NavVoxelRunDef {
	count := r.count()
	if count == 0 {
		return nil
	}
	runs := make([]NavVoxelRunDef, count)
	for i := range runs {
		runs[i] = NavVoxelRunDef{X: r.i(), Y: r.i(), Z: r.i(), Count: r.i()}
	}
	return runs
}

func collectNavGraphStrings(def *NavGraphTileDef) navStrings {
	set := map[string]struct{}{def.NavID: {}, def.AgentProfileID: {}, def.BuilderVersion: {}, def.SourceHash: {}, def.DependencyHash: {}}
	addTraversal := func(v *NavTraversalDef) {
		if v == nil {
			return
		}
		set[v.LinkID] = struct{}{}
		set[v.OwnerID] = struct{}{}
		if v.Carrier != nil {
			set[v.Carrier.CarrierID] = struct{}{}
			set[v.Carrier.FromStop] = struct{}{}
			set[v.Carrier.ToStop] = struct{}{}
			set[v.Carrier.BoardMode] = struct{}{}
			set[v.Carrier.CallControllerID] = struct{}{}
			set[v.Carrier.ControllerID] = struct{}{}
		}
	}
	for _, v := range def.SpanTransitions {
		set[v.Kind] = struct{}{}
		for _, flag := range v.RequiresFlags {
			set[flag] = struct{}{}
		}
		addTraversal(v.Traversal)
		if v.Gate != nil {
			set[v.Gate.Kind] = struct{}{}
			set[v.Gate.ID] = struct{}{}
		}
	}
	for _, v := range def.Regions {
		set[v.Area] = struct{}{}
	}
	for _, v := range def.Transitions {
		set[v.Kind] = struct{}{}
		for _, flag := range v.RequiresFlags {
			set[flag] = struct{}{}
		}
		addTraversal(v.Traversal)
		if v.Gate != nil {
			set[v.Gate.Kind] = struct{}{}
			set[v.Gate.ID] = struct{}{}
		}
	}
	return makeNavStrings(set)
}

func encodeNavGraphTile(def *NavGraphTileDef) ([]byte, error) {
	strings := collectNavGraphStrings(def)
	w := &navBinaryWriter{}
	strings.write(w)
	strings.ref(w, def.NavID)
	strings.ref(w, def.AgentProfileID)
	strings.ref(w, def.BuilderVersion)
	strings.ref(w, def.SourceHash)
	strings.ref(w, def.DependencyHash)
	w.coord(def.Coord)
	maxSpan := uint32(0)
	if len(def.SpanIDs) != 0 {
		maxSpan = def.SpanIDs[len(def.SpanIDs)-1]
	}
	words := make([]uint64, 0)
	if len(def.SpanIDs) != 0 {
		words = make([]uint64, maxSpan/64+1)
		for _, id := range def.SpanIDs {
			words[id/64] |= 1 << (id % 64)
		}
	}
	w.count(len(words))
	for _, word := range words {
		w.u64(word)
	}
	localBySpan := make([][]struct {
		ordinal int
		edge    NavSpanTransitionDef
	}, int(maxSpan)+1)
	var exceptional []struct {
		ordinal int
		edge    NavSpanTransitionDef
	}
	for i, edge := range def.SpanTransitions {
		item := struct {
			ordinal int
			edge    NavSpanTransitionDef
		}{i, edge}
		if int(edge.From) < len(localBySpan) && navBinaryOrdinaryLocalEdge(def.Coord, edge) {
			localBySpan[edge.From] = append(localBySpan[edge.From], item)
		} else {
			exceptional = append(exceptional, item)
		}
	}
	w.count(len(localBySpan) + 1)
	offset := 0
	for _, edges := range localBySpan {
		w.u32(uint32(offset))
		offset += len(edges)
	}
	w.u32(uint32(offset))
	w.count(offset)
	for _, edges := range localBySpan {
		for _, item := range edges {
			w.u32(uint32(item.ordinal))
			writeNavSpanTransition(w, strings, item.edge, false)
		}
	}
	w.count(len(exceptional))
	for _, item := range exceptional {
		w.u32(uint32(item.ordinal))
		writeNavSpanTransition(w, strings, item.edge, true)
	}
	w.count(len(def.Regions))
	for _, region := range def.Regions {
		writeNavRegion(w, strings, region)
	}
	w.count(len(def.Transitions))
	for _, transition := range def.Transitions {
		writeNavRegionTransition(w, strings, transition)
	}
	if w.err != nil {
		return nil, w.err
	}
	return wrapNavBinary(navGraphMagic, def.SchemaVersion, w.Bytes())
}

func decodeNavGraphTile(data []byte) (*NavGraphTileDef, error) {
	payload, err := unwrapNavBinary(data, navGraphMagic, CurrentNavGraphTileSchemaVersion)
	if err != nil {
		return nil, err
	}
	r := newNavBinaryReader(payload)
	strings := readNavStrings(r)
	def := &NavGraphTileDef{SchemaVersion: CurrentNavGraphTileSchemaVersion}
	def.NavID = navStringRef(r, strings)
	def.AgentProfileID = navStringRef(r, strings)
	def.BuilderVersion = navStringRef(r, strings)
	def.SourceHash = navStringRef(r, strings)
	def.DependencyHash = navStringRef(r, strings)
	def.Coord = r.coord()
	words := make([]uint64, r.count())
	for i := range words {
		words[i] = r.u64()
		for bit := uint32(0); bit < 64; bit++ {
			if words[i]&(1<<bit) != 0 {
				def.SpanIDs = append(def.SpanIDs, uint32(i)*64+bit)
			}
		}
	}
	offsets := make([]uint32, r.count())
	for i := range offsets {
		offsets[i] = r.u32()
	}
	localCount := r.count()
	transitionCount := localCount
	type orderedEdge struct {
		ordinal uint32
		edge    NavSpanTransitionDef
	}
	edges := make([]orderedEdge, 0, localCount)
	for from := 0; from+1 < len(offsets); from++ {
		if offsets[from] > offsets[from+1] || offsets[from+1] > uint32(localCount) {
			r.err = fmt.Errorf("invalid navigation CSR offsets")
			break
		}
		for index := offsets[from]; index < offsets[from+1]; index++ {
			ordinal := r.u32()
			edge := readNavSpanTransition(r, strings, false)
			edge.From = uint32(from)
			edge.To.Tile = def.Coord
			edges = append(edges, orderedEdge{ordinal, edge})
		}
	}
	if len(offsets) == 0 || offsets[len(offsets)-1] != uint32(localCount) {
		if r.err == nil {
			r.err = fmt.Errorf("invalid navigation CSR edge count")
		}
	}
	exceptionalCount := r.count()
	transitionCount += exceptionalCount
	for range exceptionalCount {
		ordinal := r.u32()
		edges = append(edges, orderedEdge{ordinal, readNavSpanTransition(r, strings, true)})
	}
	if transitionCount != 0 {
		def.SpanTransitions = make([]NavSpanTransitionDef, transitionCount)
	}
	seen := make([]bool, transitionCount)
	for _, item := range edges {
		if int(item.ordinal) >= transitionCount || seen[item.ordinal] {
			r.err = fmt.Errorf("invalid navigation transition ordinal %d", item.ordinal)
			break
		}
		def.SpanTransitions[item.ordinal] = item.edge
		seen[item.ordinal] = true
	}
	if count := r.count(); count != 0 {
		def.Regions = make([]NavRegionDef, count)
	}
	for i := range def.Regions {
		def.Regions[i] = readNavRegion(r, strings)
	}
	if count := r.count(); count != 0 {
		def.Transitions = make([]NavRegionTransitionDef, count)
	}
	for i := range def.Transitions {
		def.Transitions[i] = readNavRegionTransition(r, strings)
	}
	if err := r.done(); err != nil {
		return nil, fmt.Errorf("decode navigation graph tile: %w", err)
	}
	return def, nil
}

func navBinaryOrdinaryLocalEdge(coord TerrainChunkCoordDef, edge NavSpanTransitionDef) bool {
	return edge.To.Tile == coord && edge.Traversal == nil && edge.Gate == nil && len(edge.RequiresFlags) == 0 && (edge.Kind == NavTransitionWalk || edge.Kind == NavTransitionStep || edge.Kind == NavTransitionStair)
}

func writeNavSpanTransition(w *navBinaryWriter, s navStrings, v NavSpanTransitionDef, includeRefs bool) {
	if includeRefs {
		w.u32(v.From)
		w.coord(v.To.Tile)
	}
	w.u32(v.To.Span)
	s.ref(w, v.Kind)
	w.f32(v.StepDelta)
	w.f32(v.Width)
	w.f32(v.MinHeadroom)
	w.f32(v.MinClearance)
	w.f32(v.Cost)
	w.count(len(v.RequiresFlags))
	for _, flag := range v.RequiresFlags {
		s.ref(w, flag)
	}
	writeNavTraversal(w, s, v.Traversal)
	writeNavGate(w, s, v.Gate)
}
func readNavSpanTransition(r *navBinaryReader, s []string, includeRefs bool) NavSpanTransitionDef {
	v := NavSpanTransitionDef{}
	if includeRefs {
		v.From = r.u32()
		v.To.Tile = r.coord()
	}
	v.To.Span = r.u32()
	v.Kind = navStringRef(r, s)
	v.StepDelta = r.f32()
	v.Width = r.f32()
	v.MinHeadroom = r.f32()
	v.MinClearance = r.f32()
	v.Cost = r.f32()
	if count := r.count(); count != 0 {
		v.RequiresFlags = make([]string, count)
	}
	for i := range v.RequiresFlags {
		v.RequiresFlags[i] = navStringRef(r, s)
	}
	v.Traversal = readNavTraversal(r, s)
	v.Gate = readNavGate(r, s)
	return v
}
func writeNavTraversal(w *navBinaryWriter, s navStrings, v *NavTraversalDef) {
	if v == nil {
		w.u8(0)
		return
	}
	w.u8(1)
	s.ref(w, v.LinkID)
	s.ref(w, v.OwnerID)
	w.vec3(v.Start)
	w.vec3(v.Apex)
	w.vec3(v.End)
	w.f32(v.Speed)
	w.f32(v.LaunchSpeed)
	w.f32(v.Duration)
	if v.Carrier == nil {
		w.u8(0)
		return
	}
	w.u8(1)
	c := v.Carrier
	s.ref(w, c.CarrierID)
	s.ref(w, c.FromStop)
	s.ref(w, c.ToStop)
	w.vec3(c.Board)
	s.ref(w, c.BoardMode)
	s.ref(w, c.CallControllerID)
	s.ref(w, c.ControllerID)
}
func readNavTraversal(r *navBinaryReader, s []string) *NavTraversalDef {
	if r.u8() == 0 {
		return nil
	}
	v := &NavTraversalDef{LinkID: navStringRef(r, s), OwnerID: navStringRef(r, s), Start: r.vec3(), Apex: r.vec3(), End: r.vec3(), Speed: r.f32(), LaunchSpeed: r.f32(), Duration: r.f32()}
	if r.u8() != 0 {
		v.Carrier = &NavCarrierTraversalDef{CarrierID: navStringRef(r, s), FromStop: navStringRef(r, s), ToStop: navStringRef(r, s), Board: r.vec3(), BoardMode: navStringRef(r, s), CallControllerID: navStringRef(r, s), ControllerID: navStringRef(r, s)}
	}
	return v
}
func writeNavGate(w *navBinaryWriter, s navStrings, v *NavTransitionGateDef) {
	if v == nil {
		w.u8(0)
		return
	}
	w.u8(1)
	s.ref(w, v.Kind)
	s.ref(w, v.ID)
}
func readNavGate(r *navBinaryReader, s []string) *NavTransitionGateDef {
	if r.u8() == 0 {
		return nil
	}
	return &NavTransitionGateDef{Kind: navStringRef(r, s), ID: navStringRef(r, s)}
}
func writeNavRegion(w *navBinaryWriter, s navStrings, v NavRegionDef) {
	w.u32(v.ID)
	w.count(len(v.SpanRuns))
	for _, run := range v.SpanRuns {
		w.u32(run.Start)
		w.u32(run.Count)
	}
	w.vec3(v.BoundsMin)
	w.vec3(v.BoundsMax)
	w.vec3(v.Center)
	w.f32(v.HeightMin)
	w.f32(v.HeightMax)
	s.ref(w, v.Area)
}
func readNavRegion(r *navBinaryReader, s []string) NavRegionDef {
	v := NavRegionDef{ID: r.u32()}
	if count := r.count(); count != 0 {
		v.SpanRuns = make([]NavSpanRunDef, count)
	}
	for i := range v.SpanRuns {
		v.SpanRuns[i] = NavSpanRunDef{Start: r.u32(), Count: r.u32()}
	}
	v.BoundsMin = r.vec3()
	v.BoundsMax = r.vec3()
	v.Center = r.vec3()
	v.HeightMin = r.f32()
	v.HeightMax = r.f32()
	v.Area = navStringRef(r, s)
	return v
}
func writeNavRegionTransition(w *navBinaryWriter, s navStrings, v NavRegionTransitionDef) {
	w.u32(v.ID)
	w.u32(v.FromRegion)
	w.coord(v.ToTile)
	w.u32(v.ToRegion)
	s.ref(w, v.Kind)
	w.vec3(v.CrossingStart)
	w.vec3(v.CrossingEnd)
	w.f32(v.Width)
	w.f32(v.MinHeadroom)
	w.f32(v.MinClearance)
	w.f32(v.Cost)
	w.count(len(v.RequiresFlags))
	for _, flag := range v.RequiresFlags {
		s.ref(w, flag)
	}
	writeNavTraversal(w, s, v.Traversal)
	writeNavGate(w, s, v.Gate)
}
func readNavRegionTransition(r *navBinaryReader, s []string) NavRegionTransitionDef {
	v := NavRegionTransitionDef{ID: r.u32(), FromRegion: r.u32(), ToTile: r.coord(), ToRegion: r.u32(), Kind: navStringRef(r, s), CrossingStart: r.vec3(), CrossingEnd: r.vec3(), Width: r.f32(), MinHeadroom: r.f32(), MinClearance: r.f32(), Cost: r.f32()}
	if count := r.count(); count != 0 {
		v.RequiresFlags = make([]string, count)
	}
	for i := range v.RequiresFlags {
		v.RequiresFlags[i] = navStringRef(r, s)
	}
	v.Traversal = readNavTraversal(r, s)
	v.Gate = readNavGate(r, s)
	return v
}
