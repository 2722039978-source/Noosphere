import bpy
from pathlib import Path
BUILD_ROOT = Path(__file__).resolve().parents[1]
ROOT = BUILD_ROOT
source = (ROOT/'art/build_archive.py').read_text(encoding='utf-8')
prefix = source.split('# Convert text, bake modifiers')[0]
prefix = prefix.replace("scene = bpy.data.scenes.new('Rhine_Archive_Work')", "scene = bpy.data.scenes.new('Rhine_Assembly_Work')")
prefix = prefix.replace("if old != scene and old.name.startswith('Rhine_Archive_Asset'):", "if False:")
prefix = prefix.replace("scene.name='Rhine_Archive_Asset'", "scene.name='Rhine_Assembly_Asset'")
exec(compile(prefix, str(ROOT/'art/build_archive.py'), 'exec'))
ROOT = BUILD_ROOT
def part_for(name):
    if name.startswith(('Rear translucent carrier', 'Polished perimeter rail', 'Ivory spine cap')):
        return 'carrier'
    if name.startswith('Information substrate'): return 'substrate'
    if name.startswith(('Embedded optical cavity', 'Embedded amber annulus')): return 'optical-core'
    if name.startswith(('Subsurface refractive shoulder', 'Inner optical bevel', 'Concentric optical machining')):
        return 'optical-lenses'
    if name.startswith(('Countersunk washer', 'Machined screw', 'Screw slot')): return 'fasteners'
    return 'cover'
for obj in list(scene.objects):
    obj['assemblyPart'] = part_for(obj.name)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    if obj.type in ['FONT','CURVE']: bpy.ops.object.convert(target='MESH')
    for mod in list(obj.modifiers):
        bpy.ops.object.modifier_apply(modifier=mod.name)
    if obj.data.materials[0].name.split('.')[0] == 'Carbon_Ink':
        bpy.data.objects.remove(obj, do_unlink=True)
pairs = {}
for obj in scene.objects:
    key = (obj['assemblyPart'], obj.data.materials[0].name)
    pairs.setdefault(key, []).append(obj)
for (part, surface), objects in pairs.items():
    bpy.ops.object.select_all(action='DESELECT')
    for obj in objects: obj.select_set(True)
    bpy.context.view_layer.objects.active = objects[0]
    bpy.ops.object.join()
    obj = bpy.context.object
    obj.name = part+'__'+surface.split('.')[0]
    obj['assemblyPart'] = part
    scene.cursor.location = (0,0,0)
    bpy.ops.object.origin_set(type='ORIGIN_CURSOR')
    bpy.ops.object.transform_apply(location=False, rotation=True, scale=True)
    for vertex in obj.data.vertices: vertex.co.y *= 2.0
bpy.ops.object.select_all(action='SELECT')
bpy.ops.export_scene.gltf(filepath=str(ROOT/'public/assets/archive-assembly.glb'), export_format='GLB', use_selection=True, use_active_scene=True, export_apply=True, export_extras=True)
bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/'art/archive-assembly.blend'))
print('Assembly exported:', len(scene.objects), 'meshes, parts:', sorted({o['assemblyPart'] for o in scene.objects}))
