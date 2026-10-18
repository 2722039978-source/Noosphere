import bpy, math, os
from mathutils import Vector
from pathlib import Path

ROOT = str(Path(__file__).resolve().parents[1])
scene = bpy.data.scenes.new('Rhine_Archive_Work')
bpy.context.window.scene = scene
for old in list(bpy.data.scenes):
    if old != scene and old.name.startswith('Rhine_Archive_Asset'):
        for obj in list(old.objects):
            if len(obj.users_scene)==1:bpy.data.objects.remove(obj,do_unlink=True)
        bpy.data.scenes.remove(old)
scene.name='Rhine_Archive_Asset'
for m in list(bpy.data.materials):
    if m.users==0:bpy.data.materials.remove(m)

def material(name, color, rough=.3, metal=0, transmission=0):
    m=bpy.data.materials.new(name); m.diffuse_color=(*color,1); m.use_nodes=True
    p=m.node_tree.nodes.get('Principled BSDF')
    p.inputs['Base Color'].default_value=(*color,1)
    p.inputs['Roughness'].default_value=rough
    p.inputs['Metallic'].default_value=metal
    p.inputs['Transmission Weight'].default_value=transmission
    p.inputs['IOR'].default_value=1.46
    return m

shell=material('Frosted_Polymer',(.985,.975,.963),.36,0,.78)
edge=material('Ivory_Edges',(.94,.916,.892),.28,.02,.72)
core=material('Internal_Ceramic',(.74,.705,.68),.52,.06)
metal=material('Titanium_Fasteners',(.58,.60,.61),.19,.82)
gold=material('Champagne_Index',(.64,.46,.29),.33,.48)
paper=material('Printed_Label',(.91,.89,.84),.65)
diffuser=material('Optical_Diffuser',(.925,.902,.881),.67,0,0)
optics=material('Subsurface_Optics',(.70,.675,.66),.39,.12)
optical_edge=material('Optical_Edges',(.94,.92,.90),.3,.03,.45)
ink=material('Carbon_Ink',(.025,.026,.023),.75)

def cube(name, loc, size, mat, bevel=.015):
    bpy.ops.mesh.primitive_cube_add(size=1, location=loc)
    o=bpy.context.object; o.name=name; o.dimensions=size
    bpy.ops.object.transform_apply(location=False, rotation=False, scale=True)
    o.data.materials.append(mat)
    if bevel:
        m=o.modifiers.new('Precision radiused edge','BEVEL'); m.width=bevel; m.segments=3
        bpy.context.view_layer.objects.active=o; bpy.ops.object.modifier_apply(modifier=m.name)
        o.modifiers.new('Weighted corner normals','WEIGHTED_NORMAL')
    return o

def torus(name,x,z,radius,tube,mat,y=-.091):
    bpy.ops.mesh.primitive_torus_add(major_radius=radius,minor_radius=tube,major_segments=64,minor_segments=10,location=(x,y,z),rotation=(math.pi/2,0,0))
    o=bpy.context.object; o.name=name; o.data.materials.append(mat)
    for p in o.data.polygons:p.use_smooth=True
    return o

def text(name, body,x,z,size,mat=ink):
    c=bpy.data.curves.new(name,'FONT'); c.body=body;c.size=size;c.extrude=.0002;c.space_character=1.05
    o=bpy.data.objects.new(name,c);scene.collection.objects.link(o)
    o.location=(x,-.123,z);o.rotation_euler=(math.pi/2,0,0);c.materials.append(mat)
    return o

cube('Front frosted optical cover',(0,-.095,1.85),(5,.016,3.7),shell,.007)
cube('Rear translucent carrier',(0,.055,1.85),(4.97,.02,3.68),edge,.009)
cube('Information substrate',(0,.025,1.86),(4.80,.012,3.47),diffuser,.006)
for z in [.028,3.672]:cube('Polished perimeter rail',(0,-.021,z),(4.95,.155,.034),edge,.009)
for x in [-2.476,2.476]:cube('Polished perimeter rail',(x,-.021,1.85),(.034,.155,3.66),edge,.009)
# Wide optical cavities sit BEHIND the frosted cover. Their lenticular profiles
# are shallow; no torus protrudes from the exterior face.
for x,z,r in [(-.44,1.92,.79),(1.13,2.48,.435)]:
    o=torus('Embedded optical cavity',x,z,r,.115 if r>.5 else .071,optics,.001)
    o.scale.z=.16
    o=torus('Subsurface refractive shoulder',x,z,r+.066,.040,optical_edge,-.005)
    o.scale.z=.18
    o=torus('Inner optical bevel',x,z,r-.090,.029,optical_edge,-.018)
    o.scale.z=.20
    for offset,tube,y in [(.105,.018,-.03),(.025,.018,-.035),(-.125,.014,-.042)]:
        o=torus('Concentric optical machining',x,z,r+offset,tube,optical_edge,y)
        o.scale.z=.35
    if r<.5:
        o=torus('Embedded amber annulus',x,z,r-.11,.043,gold,-.021)
        o.scale.z=.17
for x in [-2.34,2.34]:
    for z in [.17,3.53]:
        torus('Countersunk washer',x,z,.048,.014,edge,-.095)
        bpy.ops.mesh.primitive_uv_sphere_add(segments=16,ring_count=8,radius=1,location=(x,-.105,z))
        o=bpy.context.object;o.name='Machined screw';o.scale=(.039,.012,.039);o.data.materials.append(metal)
        cut=cube('Screw slot',(x,-.12,z),(.047,.006,.008),core,.002);cut.rotation_euler.y=-.65
cube('Index tab',(-2.08,-.081,3.48),(.26,.1,.29),gold,.005)
cube('Serial label',(-1.36,-.099,3.04),(.99,.02,.41),paper,.003)
cube('Label top rule',(-1.36,-.116,3.23),(.98,.004,.008),ink,0)
cube('Label bottom rule',(-1.36,-.116,2.847),(.98,.004,.005),ink,0)
text('Company label','RHINE LAB, LLC.',-1.825,3.105,.105)
text('Database label','INTERNAL DATABASE',-1.825,3.017,.042,core)
text('Serial number','NO.001',-1.825,2.875,.148)
text('Information label','INFO',-.99,3.075,.076)
text('Symbol label','+ / -',-.99,2.9,.095)
for i in range(16):
    o=cube('Laser etched vent',(1.04+i*.054,-.111,.57),(.023,.009,.1),core,.003);o.rotation_euler.y=.4
for i in range(25):cube('Calibration mark',(-2.23,-.108,.61+i*.052),(.035 if i%5 else .075,.005,.006),core,0)
text('Edge inscription','R H I N E  L A B',-1.81,.28,.063,core)
for z,x1,x2 in [(3.36,-1.8,.2),(3.36,.4,1.55),(.4,-1.45,1.75)]:
    cube('Engraved circuit trace',((x1+x2)/2,-.108,z),(x2-x1,.004,.005),core,.002)
for depth in [-.108,-.112]:
    curve=bpy.data.curves.new('Moulded circuit channel','CURVE');curve.dimensions='3D';curve.bevel_depth=.004;curve.bevel_resolution=2
    spline=curve.splines.new('BEZIER')
    points=[(-2.3,3.18),(-2.28,3.32),(-2.03,3.47),(-.7,3.47),(-.5,3.54),(.2,3.54),(.4,3.45),(1.7,3.45),(1.87,3.51),(2.24,3.51)]
    spline.bezier_points.add(len(points)-1)
    for point,(x,z) in zip(spline.bezier_points,points):point.co=(x,depth,z);point.handle_left_type='AUTO';point.handle_right_type='AUTO'
    obj=bpy.data.objects.new('Moulded circuit channel',curve);scene.collection.objects.link(obj);curve.materials.append(core)

# White, broad end caps and a warm light guide along the selected spine.
cube('Ivory spine cap',(-2.46,-.02,1.85),(.055,.155,3.68),edge,.012)
guide=material('Amber_Lightguide',(.98,.68,.31),.28,.05,.25)
cube('Amber light guide',(-2.35,-.095,1.85),(.12,.018,3.60),guide,.01)

# Convert text, bake modifiers, and group by material for efficient instancing.
bpy.ops.object.select_all(action='SELECT')
for o in list(scene.objects):
    bpy.context.view_layer.objects.active=o
    if o.type in ['FONT','CURVE']:bpy.ops.object.convert(target='MESH')
    for m in list(o.modifiers):
        try:bpy.ops.object.modifier_apply(modifier=m.name)
        except:pass
for mat in [shell,edge,core,metal,gold,paper,ink,diffuser,optics,optical_edge,guide]:
    bpy.ops.object.select_all(action='DESELECT')
    obs=[o for o in scene.objects if o.type=='MESH' and o.data.materials and o.data.materials[0]==mat]
    if not obs:continue
    for o in obs:o.select_set(True)
    bpy.context.view_layer.objects.active=obs[0];bpy.ops.object.join()
    o=bpy.context.object;o.name=mat.name
    scene.cursor.location=(0,0,0);bpy.ops.object.origin_set(type='ORIGIN_CURSOR')
    # Bake orientation to make every exported group share the same coordinate frame.
    bpy.ops.object.transform_apply(location=False,rotation=True,scale=True)
# Annotated reference: the visible end face occupies roughly half a row pitch.
for obj in scene.objects:
    if obj.type=='MESH':
        for vertex in obj.data.vertices:vertex.co.y *= 2.0
bpy.ops.object.select_all(action='SELECT')
bpy.ops.export_scene.gltf(filepath=ROOT+'/public/assets/archive-cassette.glb',export_format='GLB',use_selection=True,use_active_scene=True,export_apply=True)
bpy.ops.wm.save_as_mainfile(filepath=ROOT+'/art/rhine-archive.blend')
print('Exported archive cassette:',len(scene.objects),'material groups')
