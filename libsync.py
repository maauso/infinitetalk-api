import os
import time
import requests
import json
import base64
from pydub import AudioSegment
from pydub.silence import split_on_silence
import subprocess
from PIL import Image
from datetime import datetime

# ================= CONFIGURACIÓN =================
API_KEY = "rpa_RGG8KZFQHRGCLV47HJVJ9TK32YUK1N9SWTZPJSSX1kupax"
# ENDPOINT_ID = "ef2k83wtsgxj6z"  # Endpoint por defecto
ENDPOINT_ID = "d48rp54wrgex2l"  # Endpoint optimizado para velocidad

# La imagen base (Avatar) que se usará para todo el video
INPUT_IMAGE = "./avatar_img/source.png"
TIMESTAMP = datetime.now().strftime("%Y%m%d%H%M")
PROCESSED_IMAGE = f"{TIMESTAMP}_source_resized.png"  # Imagen redimensionada

# Tu audio largo de 5 minutos (debe estar en local)
INPUT_LONG_AUDIO = "source_audio/audio_completo_lento.wav"

# Configuración de corte
CHUNK_TARGET_SEC = 45  # Intentar hacer cortes de 50 segundos aprox
MIN_SILENCE_LEN = 500  # Mínimo 500ms de silencio para considerar corte
SILENCE_THRESH = -40   # Umbral de silencio en dBFS

# Resolución del video (InfiniteTalk soporta 480p y 720p)
# Opciones recomendadas:
#   - 384x288  (Rápido, ~25% menos procesamiento que 512x512)
#   - 512x512  (Balanceado, opción por defecto en ejemplos)
#   - 640x480  (480p estándar, calidad media-alta)
#   - 720x540  (Mayor calidad, más lento)
VIDEO_WIDTH = 384
VIDEO_HEIGHT = 576

# Carpetas temporales
TEMP_AUDIO_DIR = "temp_audios"
TEMP_VIDEO_DIR = "temp_videos"
os.makedirs(TEMP_AUDIO_DIR, exist_ok=True)
os.makedirs(TEMP_VIDEO_DIR, exist_ok=True)

# ================= FUNCIONES =================


def resize_image_with_padding(input_path, output_path, target_width, target_height):
    """
    Redimensiona la imagen al tamaño objetivo manteniendo la proporción.
    Añade padding (barras negras) si es necesario para alcanzar las dimensiones exactas.
    """
    print(f"🖼️ Procesando imagen: {input_path}")

    with Image.open(input_path) as img:
        original_width, original_height = img.size
        print(f"   Tamaño original: {original_width}x{original_height}")

        # Calcular la escala manteniendo proporciones
        scale = min(target_width / original_width,
                    target_height / original_height)
        new_width = int(original_width * scale)
        new_height = int(original_height * scale)

        # Redimensionar con alta calidad
        img_resized = img.resize(
            (new_width, new_height), Image.Resampling.LANCZOS)

        # Crear canvas del tamaño objetivo con fondo negro
        canvas = Image.new('RGB', (target_width, target_height), (0, 0, 0))

        # Centrar la imagen redimensionada en el canvas
        offset_x = (target_width - new_width) // 2
        offset_y = (target_height - new_height) // 2
        canvas.paste(img_resized, (offset_x, offset_y))

        # Guardar
        canvas.save(output_path, 'PNG')
        print(
            f"   ✅ Imagen procesada: {target_width}x{target_height} (guardada en {output_path})")


def file_to_base64(file_path):
    """Lee un archivo y lo convierte a string Base64"""
    with open(file_path, "rb") as f:
        encoded_string = base64.b64encode(f.read()).decode('utf-8')
    return encoded_string


def split_audio_smartly():
    """Corta el audio largo en trozos basándose en el silencio"""
    print("✂️ Analizando audio y buscando silencios... (esto puede tardar un poco)")
    audio = AudioSegment.from_wav(INPUT_LONG_AUDIO)

    # Si el audio es menor o igual al límite, no lo cortes
    audio_duration_sec = len(audio) / 1000
    if audio_duration_sec <= CHUNK_TARGET_SEC:
        print(
            f"ℹ️ Audio de {audio_duration_sec:.1f}s <= {CHUNK_TARGET_SEC}s, no se necesita partir.")
        # Guardar el audio completo como único chunk
        chunk_file = os.path.join(TEMP_AUDIO_DIR, "chunk_000.wav")
        audio.export(chunk_file, format="wav")
        return [chunk_file]

    # 1. Trocear por frases (silencios)
    sentences = split_on_silence(
        audio,
        min_silence_len=MIN_SILENCE_LEN,
        silence_thresh=SILENCE_THRESH,
        keep_silence=200  # Guardar 200ms de silencio al final para que no suene cortado
    )

    chunks = []
    current_chunk = AudioSegment.empty()

    # 2. Reagrupar frases hasta llegar al CHUNK_TARGET_SEC
    for sentence in sentences:
        if len(current_chunk) + len(sentence) < (CHUNK_TARGET_SEC * 1000):
            current_chunk += sentence
        else:
            if len(current_chunk) > 0:  # ✅ Evitar añadir chunks vacíos
                chunks.append(current_chunk)
            current_chunk = sentence  # Empezar nuevo chunk con la frase actual

    # Añadir el último trozo
    if len(current_chunk) > 0:
        chunks.append(current_chunk)

    print(f"✅ Audio dividido en {len(chunks)} partes seguras.")

    # 3. Guardar archivos
    chunk_files = []
    for i, chunk in enumerate(chunks):
        filename = os.path.join(TEMP_AUDIO_DIR, f"chunk_{i:03d}.wav")
        chunk.export(filename, format="wav")
        chunk_files.append(filename)

    return chunk_files


def process_runpod(chunk_file_path, image_b64, chunk_index):
    """Envía el chunk DIRECTAMENTE en Base64 (Sin subirlo a ningún lado)"""
    print(f"🚀 Codificando y enviando Chunk #{chunk_index} a RunPod...")

    # 1. Convertir audio a Base64
    audio_b64 = file_to_base64(chunk_file_path)

    # 2. Preparar el payload con Base64
    payload = {
        "input": {
            "input_type": "image",
            "person_count": "single",
            "prompt": "high quality, realistic, speaking naturally",
            "image_base64": image_b64,  # <--- Usando image_base64
            "wav_base64": audio_b64,     # <--- Usando wav_base64
            "width": VIDEO_WIDTH,
            "height": VIDEO_HEIGHT,
            "network_volume": False,
            "force_offload": False
        }
    }

    headers = {"Authorization": f"Bearer {API_KEY}",
               "Content-Type": "application/json"}

    # POST
    try:
        req = requests.post(
            f"https://api.runpod.ai/v2/{ENDPOINT_ID}/run", json=payload, headers=headers)
        response_json = req.json()

        if 'id' not in response_json:
            print(f"❌ Error en la respuesta de RunPod: {response_json}")
            return None

        job_id = response_json['id']
    except Exception as e:
        print(f"❌ Error al conectar con RunPod: {e}")
        return None

    # POLLING
    while True:
        time.sleep(5)
        status_req = requests.get(
            f"https://api.runpod.ai/v2/{ENDPOINT_ID}/status/{job_id}", headers=headers)
        status_data = status_req.json()
        status = status_data['status']

        print(f"   [Chunk {chunk_index}] Estado: {status}")

        if status == 'COMPLETED':
            video_b64 = status_data['output'].get('video')
            if video_b64:
                output_file = os.path.join(
                    TEMP_VIDEO_DIR, f"video_part_{chunk_index:03d}.mp4")
                with open(output_file, "wb") as f:
                    f.write(base64.b64decode(video_b64))
                print(f"💾 Video parcial guardado: {output_file}")
                return output_file
            else:
                print("⚠️ Completado pero sin video.")
                return None
        elif status == 'FAILED':
            error_msg = status_data.get('error', 'Error desconocido')
            print(f"❌ Falló en RunPod: {error_msg}")
            return None


def stitch_videos(video_files, first_chunk_path):
    """Une todos los videos parciales en uno final usando ffmpeg"""
    print("🧵 Uniendo videos finales...")

    # Crear lista para ffmpeg
    list_file = "files_to_join.txt"
    with open(list_file, "w") as f:
        for vid in video_files:
            # ffmpeg requiere rutas absolutas o relativas seguras
            f.write(f"file '{vid}'\n")

    # Generar nombre con timestamp
    timestamp = datetime.now().strftime("%Y%m%d%H%M")
    output_final = f"{timestamp}_VIDEO_FINAL_COMPLETO.mp4"

    # Ejecutar comando de unión (concat demuxer es rápido y no recodifica si no es necesario)
    cmd = [
        "ffmpeg", "-f", "concat", "-safe", "0",
        "-i", list_file,
        "-c", "copy", "-y",
        output_final
    ]

    subprocess.run(cmd)
    print(f"🎉 ¡VIDEO FINAL DE 5 MINUTOS LISTO!: {output_final}")
    os.remove(list_file)

# ================= MAIN =================


if __name__ == "__main__":
    start_time = time.time()
    print(
        f"⏱️ Iniciando proceso: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n")

    # 1. Redimensionar y preparar imagen
    resize_image_with_padding(
        INPUT_IMAGE, PROCESSED_IMAGE, VIDEO_WIDTH, VIDEO_HEIGHT)
    image_b64 = file_to_base64(PROCESSED_IMAGE)
    print(f"✅ Imagen codificada en base64 ({len(image_b64)} caracteres)\n")

    # 2. Cortar Audio
    chunk_paths = split_audio_smartly()

    generated_videos = []

    # 3. Procesar cada trozo
    for i, chunk_path in enumerate(chunk_paths):
        # Procesar en RunPod (la función codifica internamente)
        video_path = process_runpod(chunk_path, image_b64, i)
        if video_path:
            generated_videos.append(video_path)
        else:
            print(f"❌ Fallo crítico en el chunk {i}. Deteniendo.")
            break

    # 4. Unir todo
    if len(generated_videos) == len(chunk_paths):
        stitch_videos(generated_videos, chunk_paths[0])
    else:
        print("⚠️ No se generaron todas las partes, no se puede unir el video final.")

    # 5. Resumen final
    end_time = time.time()
    total_seconds = end_time - start_time
    minutes = int(total_seconds // 60)
    seconds = int(total_seconds % 60)

    print("\n" + "="*60)
    print("📊 RESUMEN DEL PROCESO")
    print("="*60)
    print(f"✅ Videos generados: {len(generated_videos)}/{len(chunk_paths)}")
    print(f"⏱️ Tiempo total: {minutes}m {seconds}s ({total_seconds:.1f}s)")
    if len(generated_videos) > 0:
        avg_time = total_seconds / len(generated_videos)
        print(f"⚡ Promedio por chunk: {avg_time:.1f}s")
    print(f"🎬 Resolución: {VIDEO_WIDTH}x{VIDEO_HEIGHT}")
    print(f"🕐 Finalizado: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print("="*60)
