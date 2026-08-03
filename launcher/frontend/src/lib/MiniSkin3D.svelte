<script lang="ts">
  // Міні-3D превʼю картки Гардероба. Рендерить СПРАВЖНЮ модель
  // (skin-model-launcher.glb / -slim.glb) з текстурою скіна через
  // renderSkinThumbnail (wardrobe3d.ts) — результат один статичний кадр
  // як data URL, без живого WebGL-контексту на кожній картці.

  import { renderSkinThumbnail } from './wardrobe3d'

  let { slim, textureUrl, capeTextureUrl }: { slim: boolean; textureUrl?: string; capeTextureUrl?: string } = $props()

  let imgSrc = $state<string | null>(null)

  $effect(() => {
    const url = textureUrl
    const cape = capeTextureUrl
    // undefined = ще не запитували текстуру (запит у польоті) — чекаємо.
    // '' = запит завершився помилкою — все одно рендеримо картку:
    // renderSkinThumbnail сам підставить фолбек-текстуру Steve замість
    // порожньої білої моделі (баг з фото 1).
    if (url === undefined) {
      imgSrc = null
      return
    }
    let alive = true
    renderSkinThumbnail({ slim, textureUrl: url || undefined, capeTextureUrl: cape || undefined })
      .then((d) => {
        if (alive) imgSrc = d
      })
      .catch(() => {
        if (alive) imgSrc = null
      })
    return () => {
      alive = false
    }
  })
</script>

{#if imgSrc}
  <img class="wr-mini-3d" src={imgSrc} alt="" draggable="false" />
{/if}

<style>
  .wr-mini-3d { width:100%; height:100%; object-fit:contain; display:block; user-select:none; -webkit-user-drag:none; }
</style>
