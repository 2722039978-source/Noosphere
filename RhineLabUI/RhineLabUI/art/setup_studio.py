import bpy
from mathutils import Vector
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
s=bpy.context.scene
for obj in list(s.objects):
    if obj.type in ['CAMERA','LIGHT']:bpy.data.objects.remove(obj,do_unlink=True)
bpy.ops.object.camera_add(location=(-7,-11,6))
c=bpy.context.object;c.name='Asset_Review_Camera'
c.rotation_euler=((Vector((0,0,1.9))-c.location).to_track_quat('-Z','Y').to_euler())
c.data.type='ORTHO';c.data.ortho_scale=7.3;s.camera=c
s.world=bpy.data.worlds.new('Warm_Studio_World');s.world.use_nodes=True
s.world.node_tree.nodes['Background'].inputs[0].default_value=(.8,.77,.72,1)
s.world.node_tree.nodes['Background'].inputs[1].default_value=.65
for name,loc,power,size in [('Softbox_Key',(-4,-5,8),950,7),('Softbox_Rim',(5,1,6),1100,5),('Softbox_Fill',(1,-7,3),350,6)]:
    bpy.ops.object.light_add(type='AREA',location=loc)
    light=bpy.context.object;light.name=name;light.data.energy=power;light.data.shape='DISK';light.data.size=size
    light.rotation_euler=((Vector((0,0,2))-light.location).to_track_quat('-Z','Y').to_euler())
s.render.engine='CYCLES';s.cycles.samples=48;s.cycles.use_denoising=True
s.render.resolution_x=1200;s.render.resolution_y=1000;s.render.resolution_percentage=100
s.render.film_transparent=False;s.render.filepath=str(ROOT/'art/archive-studio.png')
bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/'art/rhine-archive.blend'))
